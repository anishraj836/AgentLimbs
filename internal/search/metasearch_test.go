package search

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/crawler-monorepo/internal/crawler"
	"github.com/crawler-monorepo/internal/index"
)

func TestExtractDDGTargetURL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"/l/?uddg=https%3A%2F%2Fgolang.org%2Fpkg%2Fnet%2Fhttp%2F", "https://golang.org/pkg/net/http/"},
		{"//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Ftest", "https://example.com/test"},
		{"https://example.com/direct/page", "https://example.com/direct/page"},
		{"/html/", ""},
		{"https://duckduckgo.com/about", ""},
		{"/l/?uddg=javascript%3Aalert(1)", ""},
		{"/l/?uddg=file%3A%2F%2F%2Fetc%2Fpasswd", ""},
		{"/l/?uddg=data%3Atext%2Fhtml%2Chello", ""},
		{"/l/?uddg=https%3A%2F%2Fduckduckgo.com%2Fsecret", ""},
		{"", ""},
	}

	for _, tt := range tests {
		got := ExtractDDGTargetURL(tt.input)
		if got != tt.expected {
			t.Errorf("ExtractDDGTargetURL(%q) = %q; want %q", tt.input, got, tt.expected)
		}
	}
}

func TestParseDDGHTML(t *testing.T) {
	mockDDGHTML := `
<!DOCTYPE html>
<html>
<body>
<div class="result">
  <h2 class="result__title">
    <a class="result__a" href="/l/?uddg=https%3A%2F%2Fgolang.org%2Fdoc%2F">Go Documentation</a>
  </h2>
  <a class="result__snippet">Official documentation for the Go programming language.</a>
</div>
<div class="result">
  <h2 class="result__title">
    <a class="result__a" href="/l/?uddg=https%3A%2F%2Fexample.com%2Farticle">Example Article</a>
  </h2>
  <a class="result__snippet">An example article snippet.</a>
</div>
</body>
</html>
`

	results, err := ParseDDGHTML([]byte(mockDDGHTML))
	if err != nil {
		t.Fatalf("ParseDDGHTML failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("Expected 2 results, got %d", len(results))
	}

	if results[0].URL != "https://golang.org/doc/" {
		t.Errorf("Expected URL https://golang.org/doc/, got %s", results[0].URL)
	}
	if results[0].Title != "Go Documentation" {
		t.Errorf("Expected Title 'Go Documentation', got %s", results[0].Title)
	}
}

func TestMetasearchAdapter_SingleflightAndLiveIndexing(t *testing.T) {
	var ddgRequestCount int32

	var mockTargetServer *httptest.Server
	mockTargetServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><title>Go Concurrency Deep Dive</title></head><body><h1>Go Concurrency</h1><p>Goroutines and channels enable efficient concurrent processing.</p></body></html>`))
	}))
	defer mockTargetServer.Close()

	mockDDGServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&ddgRequestCount, 1)
		w.Header().Set("Content-Type", "text/html")
		html := fmt.Sprintf(`
<html><body>
<div class="result">
  <a class="result__a" href="/l/?uddg=%s">Go Concurrency Deep Dive</a>
  <a class="result__snippet">Goroutines and channels in Go.</a>
</div>
</body></html>`, mockTargetServer.URL)
		_, _ = w.Write([]byte(html))
	}))
	defer mockDDGServer.Close()

	eng := index.NewIndexEngine()
	crawlerClient := crawler.NewTestClientWithTransport(mockTargetServer.Client().Transport, true)
	// Pre-seed robots cache for target server
	targetURL, _ := http.NewRequest("GET", mockTargetServer.URL, nil)
	crawler.GlobalDomainCache.FetchAndCache(targetURL.URL.Hostname(), "User-agent: *\nDisallow:")

	adapter := NewMetasearchAdapter(eng).
		WithBaseURL(mockDDGServer.URL).
		WithHTTPClient(mockDDGServer.Client()).
		WithCrawlerClient(crawlerClient).
		WithTimeout(1 * time.Second).
		WithConcurrencyLimit(10)

	var wg sync.WaitGroup
	concurrentReqs := 5
	errChan := make(chan error, concurrentReqs)
	resultsChan := make(chan []HybridSearchHit, concurrentReqs)

	for i := 0; i < concurrentReqs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hits, err := adapter.Search(context.Background(), "concurrency", 5)
			if err != nil {
				errChan <- err
				return
			}
			resultsChan <- hits
		}()
	}

	wg.Wait()
	close(errChan)
	close(resultsChan)

	for err := range errChan {
		t.Fatalf("Metasearch Search returned error: %v", err)
	}

	if count := atomic.LoadInt32(&ddgRequestCount); count != 1 {
		t.Errorf("Expected singleflight to deduplicate requests to 1 DDG query, got %d", count)
	}

	for hits := range resultsChan {
		if len(hits) == 0 {
			t.Errorf("Expected non-empty search hits from live indexing")
		} else {
			if hits[0].Title == "" {
				t.Errorf("Expected non-empty title in hybrid search hit")
			}
		}
	}
}

func TestMetasearchAdapter_DeadlineTimeout(t *testing.T) {
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer slowServer.Close()

	eng := index.NewIndexEngine()
	adapter := NewMetasearchAdapter(eng).
		WithBaseURL(slowServer.URL).
		WithHTTPClient(slowServer.Client()).
		WithTimeout(50 * time.Millisecond)

	ctx := context.Background()
	hits, err := adapter.Search(ctx, "timeout test", 5)
	if err != nil {
		t.Fatalf("Search failed with unexpected error: %v", err)
	}

	// Should handle deadline timeout gracefully without crash, returning 0 hits
	if len(hits) != 0 {
		t.Errorf("Expected 0 hits when DDG query times out, got %d", len(hits))
	}
}

func TestMetasearchAdapter_SingleflightContextIsolation(t *testing.T) {
	mockDDGServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`
<html><body>
<div class="result">
  <a class="result__a" href="https://example.com/isolated">Isolated Singleflight Target</a>
  <a class="result__snippet">Snippet for isolated singleflight test.</a>
</div>
</body></html>`))
	}))
	defer mockDDGServer.Close()

	eng := index.NewIndexEngine()
	adapter := NewMetasearchAdapter(eng).
		WithBaseURL(mockDDGServer.URL).
		WithHTTPClient(mockDDGServer.Client()).
		WithTimeout(2 * time.Second)

	// Caller 1 has a very short context (10ms) that will cancel quickly
	ctx1, cancel1 := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel1()

	// Caller 2 has a long context (2s) that should complete successfully
	ctx2 := context.Background()

	var wg sync.WaitGroup
	var hits2 []HybridSearchHit
	var err2 error

	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = adapter.Search(ctx1, "isolated query", 5)
	}()

	go func() {
		defer wg.Done()
		// Slight offset to ensure caller 1 starts the singleflight Do function first
		time.Sleep(5 * time.Millisecond)
		hits2, err2 = adapter.Search(ctx2, "isolated query", 5)
	}()

	wg.Wait()

	if err2 != nil {
		t.Fatalf("Caller 2 failed due to singleflight context binding: %v", err2)
	}
	_ = hits2
}

func TestMetasearchAdapter_TopKKeyDifferentiation(t *testing.T) {
	var requestCount int32
	mockDDGServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><div class="result"><a class="result__a" href="https://example.com/test">Title</a></div></body></html>`))
	}))
	defer mockDDGServer.Close()

	eng := index.NewIndexEngine()
	adapter := NewMetasearchAdapter(eng).
		WithBaseURL(mockDDGServer.URL).
		WithHTTPClient(mockDDGServer.Client()).
		WithTimeout(2 * time.Second)

	// Different topK should result in different singleflight keys
	_, _ = adapter.Search(context.Background(), "query-diff-topk", 5)
	_, _ = adapter.Search(context.Background(), "query-diff-topk", 10)

	if count := atomic.LoadInt32(&requestCount); count != 2 {
		t.Errorf("Expected 2 separate singleflight requests for different topK values, got %d", count)
	}
}

func TestMetasearchAdapter_CancelledCallerReturnsImmediately(t *testing.T) {
	mockDDGServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body></body></html>`))
	}))
	defer mockDDGServer.Close()

	eng := index.NewIndexEngine()
	adapter := NewMetasearchAdapter(eng).
		WithBaseURL(mockDDGServer.URL).
		WithHTTPClient(mockDDGServer.Client()).
		WithTimeout(2 * time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := adapter.Search(ctx, "cancelled-query", 5)
	if err == nil {
		t.Fatalf("Expected context cancellation error, got nil")
	}
	if err != context.Canceled {
		t.Errorf("Expected context.Canceled, got: %v", err)
	}
}

func TestMetasearchAdapter_NilHTTPClientFallback(t *testing.T) {
	mockDDGServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><div class="result"><a class="result__a" href="https://example.com/test">Title</a></div></body></html>`))
	}))
	defer mockDDGServer.Close()

	eng := index.NewIndexEngine()
	adapter := NewMetasearchAdapter(eng).
		WithBaseURL(mockDDGServer.URL).
		WithHTTPClient(nil).
		WithCrawlerClient(nil)

	// Should not panic even if httpClient is nil, and uses mock server instead of real internet
	_, _ = adapter.QueryDuckDuckGo(context.Background(), "test")
}
