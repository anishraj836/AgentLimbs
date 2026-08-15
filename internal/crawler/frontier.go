package crawler

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// Allowed standard web ports for crawler safety.
var allowedPortWhitelist = map[string]bool{
	"":     true,
	"80":   true,
	"443":  true,
	"8080": true,
	"8443": true,
}

// FrontierItem represents a task in the crawler's breadth-first search frontier.
type FrontierItem struct {
	URL   string `json:"url"`
	Depth int    `json:"depth"`
}

// FrontierConfig contains initialization parameters for the link frontier.
type FrontierConfig struct {
	SeedURL         string
	MaxDepth        int
	MaxPages        int
	AllowedHost     string
	AllowSubdomains bool
	IncludePatterns []string
	ExcludePatterns []string
	QueueCapacity   int
	AllowLoopback   bool
}

// Frontier manages URL deduplication, crawl trap mitigation, and BFS queueing.
type Frontier struct {
	queue         chan FrontierItem
	seenURLs      sync.Map // string -> struct{}
	pagesQueued   atomic.Int64
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

// NewFrontier initializes a concurrent BFS link frontier from configuration.
func NewFrontier(cfg FrontierConfig) (*Frontier, error) {
	capacity := cfg.QueueCapacity
	if capacity <= 0 {
		capacity = 50000
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
		if parsed, err := url.Parse(cfg.SeedURL); err == nil {
			allowedHost = strings.ToLower(parsed.Hostname())
		}
	}

	f := &Frontier{
		queue:         make(chan FrontierItem, capacity),
		maxDepth:      cfg.MaxDepth,
		maxPages:      cfg.MaxPages,
		allowedHost:   strings.ToLower(allowedHost),
		allowSubdoms:  cfg.AllowSubdomains,
		incPatterns:   incRegexps,
		excPatterns:   excRegexps,
		allowLoopback: cfg.AllowLoopback,
	}

	if cfg.SeedURL != "" {
		if _, err := f.Enqueue(cfg.SeedURL, 0); err != nil {
			return nil, fmt.Errorf("failed to enqueue seed URL %q: %w", cfg.SeedURL, err)
		}
	}

	return f, nil
}

// patternToRegexp converts simple glob-like wildcards (* and ?) or regex strings to compiled Regexp.
func patternToRegexp(pattern string) (*regexp.Regexp, error) {
	trimmed := strings.TrimSpace(pattern)
	if trimmed == "" {
		return nil, fmt.Errorf("empty pattern")
	}

	// If it already looks like a regex with anchors or groups, try compiling directly
	if strings.HasPrefix(trimmed, "^") || strings.HasSuffix(trimmed, "$") || strings.Contains(trimmed, `\`) {
		if rx, err := regexp.Compile(trimmed); err == nil {
			return rx, nil
		}
	}

	// Convert glob wildcard to regex: replace . with \., * with .*, ? with .
	var sb strings.Builder
	for i := 0; i < len(trimmed); i++ {
		c := trimmed[i]
		switch c {
		case '*':
			sb.WriteString(".*")
		case '?':
			sb.WriteString(".")
		case '.', '+', '(', ')', '[', ']', '{', '}', '^', '$', '|', '\\':
			sb.WriteByte('\\')
			sb.WriteByte(c)
		default:
			sb.WriteByte(c)
		}
	}
	return regexp.Compile(sb.String())
}

// NormalizeCanonicalURL applies the standard WebLimbAI URL canonicalization and trap detection pipeline.
func NormalizeCanonicalURL(rawURL string, allowLoopback bool) (string, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return "", fmt.Errorf("empty URL")
	}

	if len(trimmed) > 2048 {
		return "", fmt.Errorf("URL length %d exceeds cap of 2048 characters", len(trimmed))
	}

	u, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("malformed URL %q: %w", rawURL, err)
	}

	// Scheme check
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("unsupported scheme %q (must be http or https)", u.Scheme)
	}
	u.Scheme = scheme

	// Host check
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return "", fmt.Errorf("missing host in URL %q", rawURL)
	}

	port := u.Port()
	// Strip default ports
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}

	// Whitelist port check
	if !allowLoopback && !allowedPortWhitelist[port] {
		return "", fmt.Errorf("disallowed destination port %q", port)
	}

	if port != "" {
		u.Host = host + ":" + port
	} else {
		u.Host = host
	}

	// Zero-DNS Syntactic SSRF Layer 1 check
	if ip := net.ParseIP(host); ip != nil {
		if !allowLoopback && IsPrivateIP(ip) {
			return "", fmt.Errorf("blocked request to private/internal IP %s", host)
		}
	}

	// Strip fragment
	u.Fragment = ""

	// Canonicalize Path
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}

	// Strip trailing slash on non-root paths
	if path != "/" && strings.HasSuffix(path, "/") {
		path = strings.TrimRight(path, "/")
	}

	// Crawler Trap Mitigations: Path segments limit & repetition limit
	trimmedPath := strings.Trim(path, "/")
	if trimmedPath != "" {
		segments := strings.Split(trimmedPath, "/")
		if len(segments) > 10 {
			return "", fmt.Errorf("crawler trap: path segments count %d exceeds maximum allowed (10)", len(segments))
		}

		segmentFreq := make(map[string]int)
		for _, seg := range segments {
			if seg == "" {
				continue
			}
			segmentFreq[seg]++
			if segmentFreq[seg] >= 3 {
				return "", fmt.Errorf("crawler trap: repeated path segment %q detected >= 3 times", seg)
			}
		}
	}

	u.Path = path
	u.RawPath = ""

	// Strip tracking query parameters and sort remaining query parameters
	if u.RawQuery != "" {
		q := u.Query()
		keysToRemove := make([]string, 0)
		for k := range q {
			kLower := strings.ToLower(k)
			if strings.HasPrefix(kLower, "utm_") ||
				kLower == "fbclid" ||
				kLower == "gclid" ||
				kLower == "ref" ||
				kLower == "_hsenc" ||
				kLower == "_hsmi" ||
				kLower == "mc_cid" ||
				kLower == "mc_eid" {
				keysToRemove = append(keysToRemove, k)
			}
		}
		for _, k := range keysToRemove {
			q.Del(k)
		}

		if len(q) == 0 {
			u.RawQuery = ""
		} else {
			// Sort query keys alphabetically
			keys := make([]string, 0, len(q))
			for k := range q {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			var qParts []string
			for _, k := range keys {
				vals := q[k]
				sort.Strings(vals)
				for _, v := range vals {
					qParts = append(qParts, url.QueryEscape(k)+"="+url.QueryEscape(v))
				}
			}
			u.RawQuery = strings.Join(qParts, "&")
		}
	}

	canonical := u.String()
	if len(canonical) > 2048 {
		return "", fmt.Errorf("canonical URL length %d exceeds cap of 2048 characters", len(canonical))
	}

	// Drop media resource extensions
	if IsMediaResourceURL(canonical) {
		return "", fmt.Errorf("blocked media resource URL: %s", canonical)
	}

	return canonical, nil
}

// IsAllowedHost checks if the target host conforms to allowed domain and subdomain rules.
func (f *Frontier) IsAllowedHost(targetURL string) bool {
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

// MatchesPatterns checks inclusion and exclusion filter patterns.
func (f *Frontier) MatchesPatterns(targetURL string) bool {
	u, err := url.Parse(targetURL)
	if err != nil {
		return false
	}
	urlStr := u.String()
	pathStr := u.Path

	// If include patterns specified, at least one must match
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

	// If exclude patterns specified, none must match
	if len(f.excPatterns) > 0 {
		for _, rx := range f.excPatterns {
			if rx.MatchString(urlStr) || rx.MatchString(pathStr) {
				return false
			}
		}
	}

	return true
}

// Enqueue normalizes, verifies boundaries, deduplicates atomically, and pushes to the BFS queue.
func (f *Frontier) Enqueue(rawURL string, depth int) (bool, error) {
	if f.closed.Load() {
		return false, fmt.Errorf("frontier is closed")
	}

	if f.maxDepth > 0 && depth > f.maxDepth {
		return false, nil // Depth exceeded, silently skip
	}

	normalized, err := NormalizeCanonicalURL(rawURL, f.allowLoopback)
	if err != nil {
		return false, err
	}

	if !f.IsAllowedHost(normalized) {
		return false, nil
	}

	if !f.MatchesPatterns(normalized) {
		return false, nil
	}

	// Atomic seen test-and-set
	if _, loaded := f.seenURLs.LoadOrStore(normalized, struct{}{}); loaded {
		return false, nil // Already seen
	}

	select {
	case f.queue <- FrontierItem{URL: normalized, Depth: depth}:
		f.pagesQueued.Add(1)
		return true, nil
	default:
		// Queue is full (backpressure mitigation)
		return false, fmt.Errorf("frontier queue capacity exceeded")
	}
}

// Dequeue retrieves the next item from the BFS queue with context cancellation support.
func (f *Frontier) Dequeue(ctx context.Context) (FrontierItem, bool) {
	select {
	case <-ctx.Done():
		return FrontierItem{}, false
	case item, ok := <-f.queue:
		return item, ok
	}
}

// TryDequeue attempts an immediate non-blocking dequeue operation.
func (f *Frontier) TryDequeue() (FrontierItem, bool) {
	select {
	case item, ok := <-f.queue:
		return item, ok
	default:
		return FrontierItem{}, false
	}
}

// Len returns the current count of items pending in the queue channel.
func (f *Frontier) Len() int {
	return len(f.queue)
}

// PagesQueued returns the total count of unique pages enqueued throughout the lifecycle.
func (f *Frontier) PagesQueued() int64 {
	return f.pagesQueued.Load()
}

// IsSeen returns whether a normalized URL has already been processed or enqueued.
func (f *Frontier) IsSeen(rawURL string) bool {
	normalized, err := NormalizeCanonicalURL(rawURL, f.allowLoopback)
	if err != nil {
		return false
	}
	_, seen := f.seenURLs.Load(normalized)
	return seen
}

// Close gracefully closes the frontier queue channel.
func (f *Frontier) Close() {
	f.closeOnce.Do(func() {
		f.closed.Store(true)
		close(f.queue)
	})
}
