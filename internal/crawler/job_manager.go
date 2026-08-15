package crawler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/crawler-monorepo/common/stemmer"
	"github.com/crawler-monorepo/common/stopwords"
	"github.com/crawler-monorepo/internal/extractor"
	"github.com/crawler-monorepo/internal/index"
	"github.com/google/uuid"
	"golang.org/x/time/rate"
)

// CrawlRequest defines configuration and boundaries for recursive whole-domain crawling.
type CrawlRequest struct {
	URL              string            `json:"url"`
	MaxDepth         int               `json:"max_depth"`
	MaxPages         int               `json:"max_pages"`
	IncludePatterns  []string          `json:"include_patterns,omitempty"`
	ExcludePatterns  []string          `json:"exclude_patterns,omitempty"`
	AllowSubdomains  bool              `json:"allow_subdomains,omitempty"`
	SitemapDiscovery bool              `json:"sitemap_discovery,omitempty"`
	Concurrency      int               `json:"concurrency,omitempty"`
	RateLimitRPS     float64           `json:"rate_limit_rps,omitempty"`
	Async            bool              `json:"async,omitempty"`
	Headers          map[string]string `json:"headers,omitempty"`
	Cookies          map[string]string `json:"cookies,omitempty"`
	AllowLoopback    bool              `json:"allow_loopback,omitempty"`
}

// CrawlPageResult records the outcome and token savings of an individual crawled page.
type CrawlPageResult struct {
	URL            string  `json:"url"`
	Title          string  `json:"title"`
	Tokens         int     `json:"tokens"`
	RawTokens      int     `json:"raw_tokens"`
	SavingsPct     float64 `json:"savings_pct"`
	Depth          int     `json:"depth"`
	StatusCode     int     `json:"status_code"`
	LatencyMs      float64 `json:"latency_ms"`
	DiscoveredURLs int     `json:"discovered_urls"`
}

// CrawlJob tracks the runtime state, atomic counters, and error logs of an active or finished crawl.
type CrawlJob struct {
	ID           string            `json:"id"`
	Status       string            `json:"status"` // "running", "completed", "failed", "cancelled"
	Request      CrawlRequest      `json:"request"`
	PagesCrawled atomic.Int64      `json:"pages_crawled"`
	PagesQueued  atomic.Int64      `json:"pages_queued"`
	TokensSaved  atomic.Int64      `json:"tokens_saved"`
	ErrorsCount  atomic.Int64      `json:"errors_count"`
	RecentErrors []string          `json:"recent_errors"`
	Results      []CrawlPageResult `json:"results,omitempty"`
	StartTime    time.Time         `json:"start_time"`
	EndTime      *time.Time        `json:"end_time,omitempty"`
	CancelFunc   context.CancelFunc `json:"-"`
	mu           sync.RWMutex      `json:"-"`
}

func (j *CrawlJob) AddError(errStr string) {
	j.ErrorsCount.Add(1)
	j.mu.Lock()
	defer j.mu.Unlock()
	if len(j.RecentErrors) >= 10 {
		j.RecentErrors = j.RecentErrors[1:]
	}
	j.RecentErrors = append(j.RecentErrors, errStr)
}

func (j *CrawlJob) AddResult(res CrawlPageResult) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.Results = append(j.Results, res)
}

func (j *CrawlJob) SetStatus(status string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.Status = status
	now := time.Now()
	j.EndTime = &now
}

func (j *CrawlJob) GetStatus() string {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.Status
}

// MarshalJSON provides safe concurrent serialization of atomic counters and locked slices.
func (j *CrawlJob) MarshalJSON() ([]byte, error) {
	j.mu.RLock()
	defer j.mu.RUnlock()

	type Alias CrawlJob
	return json.Marshal(&struct {
		*Alias
		PagesCrawled int64             `json:"pages_crawled"`
		PagesQueued  int64             `json:"pages_queued"`
		TokensSaved  int64             `json:"tokens_saved"`
		ErrorsCount  int64             `json:"errors_count"`
		RecentErrors []string          `json:"recent_errors"`
		Results      []CrawlPageResult `json:"results,omitempty"`
	}{
		Alias:        (*Alias)(j),
		PagesCrawled: j.PagesCrawled.Load(),
		PagesQueued:  j.PagesQueued.Load(),
		TokensSaved:  j.TokensSaved.Load(),
		ErrorsCount:  j.ErrorsCount.Load(),
		RecentErrors: j.RecentErrors,
		Results:      j.Results,
	})
}

// UnmarshalJSON provides safe deserialization into atomic counters and locked fields.
func (j *CrawlJob) UnmarshalJSON(data []byte) error {
	type Alias struct {
		ID           string            `json:"id"`
		Status       string            `json:"status"`
		Request      CrawlRequest      `json:"request"`
		PagesCrawled int64             `json:"pages_crawled"`
		PagesQueued  int64             `json:"pages_queued"`
		TokensSaved  int64             `json:"tokens_saved"`
		ErrorsCount  int64             `json:"errors_count"`
		RecentErrors []string          `json:"recent_errors"`
		Results      []CrawlPageResult `json:"results,omitempty"`
		StartTime    time.Time         `json:"start_time"`
		EndTime      *time.Time        `json:"end_time,omitempty"`
	}

	var aux Alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	j.ID = aux.ID
	j.Status = aux.Status
	j.Request = aux.Request
	j.PagesCrawled.Store(aux.PagesCrawled)
	j.PagesQueued.Store(aux.PagesQueued)
	j.TokensSaved.Store(aux.TokensSaved)
	j.ErrorsCount.Store(aux.ErrorsCount)
	j.RecentErrors = aux.RecentErrors
	j.Results = aux.Results
	j.StartTime = aux.StartTime
	j.EndTime = aux.EndTime

	return nil
}

// JobManager manages asynchronous crawl jobs, per-host rate limiting, and 1-hour TTL eviction.
type JobManager struct {
	jobs          sync.Map // string -> *CrawlJob
	rateLimiters  sync.Map // string -> *rate.Limiter
	httpClient    *Client
	dataDir       string
	ttl           time.Duration
	maxRetained   int
	closeChan     chan struct{}
	closeOnce     sync.Once
}

// GlobalJobManager is the default global singleton instance for the process.
var GlobalJobManager = NewJobManager(NewClient(), "data")

// NewJobManager initializes a JobManager and starts its background TTL eviction janitor.
func NewJobManager(httpClient *Client, dataDir string) *JobManager {
	if httpClient == nil {
		httpClient = NewClient()
	}
	if dataDir == "" {
		dataDir = "data"
	}

	jm := &JobManager{
		httpClient:  httpClient,
		dataDir:     dataDir,
		ttl:         1 * time.Hour,
		maxRetained: 1000,
		closeChan:   make(chan struct{}),
	}

	go jm.startJanitor()
	return jm
}

func (m *JobManager) startJanitor() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-m.closeChan:
			return
		case <-ticker.C:
			m.EvictExpiredJobs()
		}
	}
}

// EvictExpiredJobs removes completed, failed, or cancelled jobs older than 1 hour or beyond retention cap.
func (m *JobManager) EvictExpiredJobs() {
	now := time.Now()
	var allJobs []*CrawlJob

	m.jobs.Range(func(key, value interface{}) bool {
		job := value.(*CrawlJob)
		allJobs = append(allJobs, job)
		return true
	})

	// Evict by TTL
	for _, job := range allJobs {
		job.mu.RLock()
		status := job.Status
		endTime := job.EndTime
		job.mu.RUnlock()

		if (status == "completed" || status == "failed" || status == "cancelled") && endTime != nil {
			if now.Sub(*endTime) > m.ttl {
				m.jobs.Delete(job.ID)
			}
		}
	}

	// Evict by MaxRetained cap if necessary
	count := 0
	m.jobs.Range(func(k, v interface{}) bool {
		count++
		return true
	})

	if count > m.maxRetained {
		for _, job := range allJobs {
			if count <= m.maxRetained {
				break
			}
			job.mu.RLock()
			status := job.Status
			job.mu.RUnlock()
			if status != "running" {
				m.jobs.Delete(job.ID)
				count--
			}
		}
	}
}

// GetJob retrieves a CrawlJob by its unique identifier.
func (m *JobManager) GetJob(jobID string) (*CrawlJob, bool) {
	val, ok := m.jobs.Load(jobID)
	if !ok {
		return nil, false
	}
	return val.(*CrawlJob), true
}

// CancelJob cancels an inflight crawl job.
func (m *JobManager) CancelJob(jobID string) bool {
	val, ok := m.jobs.Load(jobID)
	if !ok {
		return false
	}
	job := val.(*CrawlJob)
	if job.CancelFunc != nil {
		job.CancelFunc()
	}
	job.SetStatus("cancelled")
	return true
}

// ListJobs returns all active and retained crawl jobs.
func (m *JobManager) ListJobs() []*CrawlJob {
	var list []*CrawlJob
	m.jobs.Range(func(k, v interface{}) bool {
		list = append(list, v.(*CrawlJob))
		return true
	})
	return list
}

func (m *JobManager) getRateLimiter(host string, rps float64, concurrency int) *rate.Limiter {
	if rps <= 0 {
		rps = 10.0
	}
	burst := concurrency / 2
	if burst < 1 {
		burst = 1
	}

	val, loaded := m.rateLimiters.Load(host)
	if loaded {
		return val.(*rate.Limiter)
	}

	limiter := rate.NewLimiter(rate.Limit(rps), burst)
	actual, _ := m.rateLimiters.LoadOrStore(host, limiter)
	return actual.(*rate.Limiter)
}

// StartCrawl initiates a whole-domain crawl job (either synchronous or asynchronous).
func (m *JobManager) StartCrawl(ctx context.Context, req CrawlRequest) (*CrawlJob, error) {
	if req.URL == "" {
		return nil, fmt.Errorf("seed URL is required")
	}

	if req.MaxDepth <= 0 {
		req.MaxDepth = 2
	}
	if req.MaxDepth > 10 {
		req.MaxDepth = 10
	}

	if req.MaxPages <= 0 {
		req.MaxPages = 50
	}
	if req.MaxPages > 50000 {
		req.MaxPages = 50000
	}

	if req.Concurrency <= 0 {
		req.Concurrency = 8
	}
	if req.Concurrency > 32 {
		req.Concurrency = 32
	}

	if req.RateLimitRPS <= 0 {
		req.RateLimitRPS = 10.0
	}

	seedCanonical, err := NormalizeCanonicalURL(req.URL, req.AllowLoopback)
	if err != nil {
		return nil, fmt.Errorf("invalid seed URL: %w", err)
	}

	jobID := uuid.New().String()
	jobCtx, jobCancel := context.WithCancel(context.Background())

	job := &CrawlJob{
		ID:           jobID,
		Status:       "running",
		Request:      req,
		RecentErrors: make([]string, 0),
		Results:      make([]CrawlPageResult, 0),
		StartTime:    time.Now(),
		CancelFunc:   jobCancel,
	}

	m.jobs.Store(jobID, job)

	frontier, err := NewFrontier(FrontierConfig{
		SeedURL:         seedCanonical,
		MaxDepth:        req.MaxDepth,
		MaxPages:        req.MaxPages,
		AllowSubdomains: req.AllowSubdomains,
		IncludePatterns: req.IncludePatterns,
		ExcludePatterns: req.ExcludePatterns,
		AllowLoopback:   req.AllowLoopback,
	})
	if err != nil {
		job.SetStatus("failed")
		job.AddError(err.Error())
		return job, err
	}
	job.PagesQueued.Add(frontier.PagesQueued())

	if req.Async {
		go m.executeCrawl(jobCtx, job, frontier)
		return job, nil
	}

	// Synchronous execution
	m.executeCrawl(jobCtx, job, frontier)
	return job, nil
}

func (m *JobManager) executeCrawl(ctx context.Context, job *CrawlJob, frontier *Frontier) {
	defer frontier.Close()

	// Sitemap XML expansion at startup
	if job.Request.SitemapDiscovery {
		sitemapURLs := DiscoverSitemaps(job.Request.URL)
		for _, sitemapURL := range sitemapURLs {
			if ctx.Err() != nil {
				break
			}
			links, err := FetchAndParseSitemap(ctx, m.httpClient, sitemapURL)
			if err == nil && len(links) > 0 {
				for _, link := range links {
					if enqueued, _ := frontier.Enqueue(link, 1); enqueued {
						job.PagesQueued.Add(1)
					}
				}
				break // Successfully expanded first valid sitemap
			}
		}
	}

	var activeWorkers atomic.Int32
	var wg sync.WaitGroup

	for i := 0; i < job.Request.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for {
				if ctx.Err() != nil {
					return
				}
				if job.PagesCrawled.Load() >= int64(job.Request.MaxPages) {
					return
				}

				// Quiescence check
				item, ok := frontier.TryDequeue()
				if !ok {
					if activeWorkers.Load() == 0 && frontier.Len() == 0 {
						return // Quiescent state: all workers idle and queue empty
					}
					// Short backoff before rechecking queue
					select {
					case <-ctx.Done():
						return
					case <-time.After(50 * time.Millisecond):
					}
					continue
				}

				activeWorkers.Add(1)
				m.processCrawlItem(ctx, job, frontier, item)
				activeWorkers.Add(-1)
			}
		}()
	}

	wg.Wait()

	if job.GetStatus() == "running" {
		if ctx.Err() != nil {
			job.SetStatus("cancelled")
		} else {
			job.SetStatus("completed")
		}
	}
}

func (m *JobManager) processCrawlItem(ctx context.Context, job *CrawlJob, frontier *Frontier, item FrontierItem) {
	t0 := time.Now()

	u, err := url.Parse(item.URL)
	if err != nil {
		job.AddError(fmt.Sprintf("%s: %v", item.URL, err))
		return
	}

	limiter := m.getRateLimiter(u.Hostname(), job.Request.RateLimitRPS, job.Request.Concurrency)
	if waitErr := limiter.Wait(ctx); waitErr != nil {
		return
	}

	res, fetchErr := m.httpClient.FetchWithAuth(ctx, item.URL, job.Request.Headers, job.Request.Cookies)
	if fetchErr != nil {
		job.AddError(fmt.Sprintf("%s: %v", item.URL, fetchErr))
		return
	}
	defer res.Response.Body.Close()

	if res.Response.StatusCode >= 400 {
		job.AddError(fmt.Sprintf("%s returned HTTP %d", item.URL, res.Response.StatusCode))
		return
	}

	limitedBody := io.LimitReader(res.Response.Body, 10*1024*1024)
	htmlBytes, readErr := io.ReadAll(limitedBody)
	if readErr != nil {
		job.AddError(fmt.Sprintf("%s: failed to read body: %v", item.URL, readErr))
		return
	}

	contentType := res.Response.Header.Get("Content-Type")
	mdText, tokens, title, extractErr := extractor.ExtractDocumentText(res.FinalURL, contentType, htmlBytes, "clean_rag")
	if extractErr != nil {
		job.AddError(fmt.Sprintf("%s: extraction error: %v", item.URL, extractErr))
		return
	}

	rawTokens := len(strings.Fields(string(htmlBytes)))
	savingsPct := 0.0
	if rawTokens > 0 {
		savingsPct = float64(rawTokens-tokens) / float64(rawTokens) * 100.0
	}
	if savingsPct < 0 {
		savingsPct = 0
	}

	bodyForIndexing := mdText
	docTitle := title
	if cleanDoc, _ := extractor.ProcessRawHTML(res.FinalURL, htmlBytes); cleanDoc != nil && cleanDoc.Body != "" {
		bodyForIndexing = cleanDoc.Body
		if cleanDoc.Title != "" {
			docTitle = cleanDoc.Title
		}
	}

	rawToks := strings.Fields(strings.ToLower(bodyForIndexing))
	termPositions := make(map[string][]int)
	for idx, raw := range rawToks {
		clean := strings.Trim(raw, ".,!?:;\"'()[]{}")
		if clean == "" || stopwords.IsStopword(clean) {
			continue
		}
		stemmed := stemmer.Stem(clean)
		termPositions[stemmed] = append(termPositions[stemmed], idx)
	}

	index.GlobalEngine.IndexDocumentWithSource(res.FinalURL, docTitle, bodyForIndexing, termPositions, tokens, "crawl_job", res.FinalURL)

	// Link discovery if within depth budget
	discoveredCount := 0
	if item.Depth < job.Request.MaxDepth && strings.Contains(contentType, "html") {
		doc, parseErr := goquery.NewDocumentFromReader(bytes.NewReader(htmlBytes))
		if parseErr == nil {
			doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
				href, exists := s.Attr("href")
				if exists {
					resolved := resolveRelativeURL(res.FinalURL, href)
					if resolved != "" {
						if enqueued, _ := frontier.Enqueue(resolved, item.Depth+1); enqueued {
							job.PagesQueued.Add(1)
							discoveredCount++
						}
					}
				}
			})
		}
	}

	tokensSaved := rawTokens - tokens
	if tokensSaved > 0 {
		job.TokensSaved.Add(int64(tokensSaved))
	}
	job.PagesCrawled.Add(1)

	latency := float64(time.Since(t0).Microseconds()) / 1000.0
	job.AddResult(CrawlPageResult{
		URL:            res.FinalURL,
		Title:          title,
		Tokens:         tokens,
		RawTokens:      rawTokens,
		SavingsPct:     math.Round(savingsPct*10) / 10,
		Depth:          item.Depth,
		StatusCode:     res.Response.StatusCode,
		LatencyMs:      latency,
		DiscoveredURLs: discoveredCount,
	})
}

func resolveRelativeURL(baseURL, href string) string {
	href = strings.TrimSpace(href)
	if href == "" || strings.HasPrefix(href, "javascript:") || strings.HasPrefix(href, "mailto:") || strings.HasPrefix(href, "tel:") {
		return ""
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	rel, err := url.Parse(href)
	if err != nil {
		return ""
	}
	return base.ResolveReference(rel).String()
}

// Close terminates background janitors.
func (m *JobManager) Close() {
	m.closeOnce.Do(func() {
		close(m.closeChan)
	})
}
