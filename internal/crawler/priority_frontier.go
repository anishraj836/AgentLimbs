package crawler

import (
	"container/heap"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
)

// PriorityItem represents a candidate URL task with depth and heuristic priority.
type PriorityItem struct {
	URL      string  `json:"url"`
	Depth    int     `json:"depth"`
	Priority float64 `json:"priority"`
	index    int     // Internal container/heap index
}

type priorityHeap []*PriorityItem

func (h priorityHeap) Len() int           { return len(h) }
func (h priorityHeap) Less(i, j int) bool { return h[i].Priority > h[j].Priority } // Max-Heap (Highest priority first)
func (h priorityHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}
func (h *priorityHeap) Push(x any) {
	item := x.(*PriorityItem)
	item.index = len(*h)
	*h = append(*h, item)
}
func (h *priorityHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	item.index = -1
	*h = old[0 : n-1]
	return item
}

type PriorityFrontierConfig struct {
	SeedURL         string
	MaxDepth        int
	MaxPages        int
	AllowedHost     string
	AllowSubdomains bool
	IncludePatterns []string
	ExcludePatterns []string
	QueueCapacity   int
	MinPriority     float64
	AllowLoopback   bool
}

type PriorityFrontier struct {
	mu            sync.Mutex
	heap          priorityHeap
	seenURLs      sync.Map
	pagesQueued   atomic.Int64
	capacity      int
	minPriority   float64
	maxDepth      int
	maxPages      int
	allowedHost   string
	allowSubdoms  bool
	incPatterns   []*regexp.Regexp
	excPatterns   []*regexp.Regexp
	allowLoopback bool
	closed        atomic.Bool
	closeOnce     sync.Once
}

func NewPriorityFrontier(cfg PriorityFrontierConfig) (*PriorityFrontier, error) {
	capacity := cfg.QueueCapacity
	if capacity <= 0 || capacity > 50000 {
		capacity = 50000
	}

	minPriority := cfg.MinPriority
	if minPriority <= 0 {
		minPriority = 0.10
	}

	incRegexps := make([]*regexp.Regexp, 0, len(cfg.IncludePatterns))
	for _, p := range cfg.IncludePatterns {
		if strings.TrimSpace(p) == "" {
			continue
		}
		rx, err := patternToRegexp(p)
		if err != nil {
			return nil, fmt.Errorf("invalid include_pattern %q: %w", p, err)
		}
		incRegexps = append(incRegexps, rx)
	}

	excRegexps := make([]*regexp.Regexp, 0, len(cfg.ExcludePatterns))
	for _, p := range cfg.ExcludePatterns {
		if strings.TrimSpace(p) == "" {
			continue
		}
		rx, err := patternToRegexp(p)
		if err != nil {
			return nil, fmt.Errorf("invalid exclude_pattern %q: %w", p, err)
		}
		excRegexps = append(excRegexps, rx)
	}

	allowedHost := cfg.AllowedHost
	if allowedHost == "" && cfg.SeedURL != "" {
		if u, err := url.Parse(cfg.SeedURL); err == nil {
			allowedHost = strings.ToLower(u.Hostname())
		}
	}

	f := &PriorityFrontier{
		heap:          make(priorityHeap, 0, 1024),
		capacity:      capacity,
		minPriority:   minPriority,
		maxDepth:      cfg.MaxDepth,
		maxPages:      cfg.MaxPages,
		allowedHost:   allowedHost,
		allowSubdoms:  cfg.AllowSubdomains,
		incPatterns:   incRegexps,
		excPatterns:   excRegexps,
		allowLoopback: cfg.AllowLoopback,
	}
	heap.Init(&f.heap)

	return f, nil
}

func (f *PriorityFrontier) IsAllowedHost(targetURL string) bool {
	if f.allowedHost == "" {
		return true
	}
	u, err := url.Parse(targetURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == f.allowedHost {
		return true
	}
	if f.allowSubdoms && strings.HasSuffix(host, "."+f.allowedHost) {
		return true
	}
	return false
}

func (f *PriorityFrontier) MatchesPatterns(targetURL string) bool {
	u, err := url.Parse(targetURL)
	if err != nil {
		return false
	}
	urlStr := u.String()
	pathStr := u.Path

	if len(f.incPatterns) > 0 {
		matched := false
		for _, rx := range f.incPatterns {
			if rx.MatchString(urlStr) || rx.MatchString(pathStr) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	if len(f.excPatterns) > 0 {
		for _, rx := range f.excPatterns {
			if rx.MatchString(urlStr) || rx.MatchString(pathStr) {
				return false
			}
		}
	}

	return true
}

func (f *PriorityFrontier) Enqueue(rawURL string, anchorText string, depth int) (bool, error) {
	if f == nil || f.closed.Load() {
		return false, fmt.Errorf("priority frontier closed")
	}

	if f.maxDepth > 0 && depth > f.maxDepth {
		return false, nil
	}

	normURL, err := NormalizeCanonicalURL(rawURL, f.allowLoopback)
	if err != nil {
		return false, err
	}

	if !f.IsAllowedHost(normURL) {
		return false, nil
	}

	if !f.MatchesPatterns(normURL) {
		return false, nil
	}

	priority := ComputePriority(normURL, anchorText, depth)
	if priority < f.minPriority {
		return false, nil
	}

	if math.IsNaN(priority) || math.IsInf(priority, 0) {
		priority = 0.05
	}

	// Atomic seen test-and-set
	if _, loaded := f.seenURLs.LoadOrStore(normURL, struct{}{}); loaded {
		return false, nil
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed.Load() {
		return false, fmt.Errorf("priority frontier closed")
	}

	if len(f.heap) >= f.capacity {
		return false, fmt.Errorf("priority frontier capacity exceeded")
	}

	item := &PriorityItem{
		URL:      normURL,
		Depth:    depth,
		Priority: priority,
	}
	heap.Push(&f.heap, item)
	f.pagesQueued.Add(1)

	return true, nil
}

func (f *PriorityFrontier) TryDequeue() (*PriorityItem, bool) {
	if f == nil || f.closed.Load() {
		return nil, false
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed.Load() || len(f.heap) == 0 {
		return nil, false
	}

	item := heap.Pop(&f.heap).(*PriorityItem)
	return item, true
}

func (f *PriorityFrontier) Len() int {
	if f == nil {
		return 0
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.heap)
}

func (f *PriorityFrontier) PagesQueued() int64 {
	if f == nil {
		return 0
	}
	return f.pagesQueued.Load()
}

func (f *PriorityFrontier) Close() {
	if f == nil {
		return
	}
	f.closeOnce.Do(func() {
		f.closed.Store(true)
	})
}

func (f *PriorityFrontier) IsClosed() bool {
	if f == nil {
		return true
	}
	return f.closed.Load()
}
