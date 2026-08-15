package search

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/crawler-monorepo/internal/index"
)

func TestDuckDuckGoSearchProvider_Search(t *testing.T) {
	mockHTML := `
<!DOCTYPE html>
<html><body>
<div class="result">
  <h2><a class="result__a" href="/l/?uddg=https%3A%2F%2Fgolang.org%2Fpkg">Go Packages</a></h2>
  <a class="result__snippet">Standard library package documentation for Go.</a>
</div>
<div class="result">
  <h2><a class="result__a" href="/l/?uddg=https%3A%2F%2Fgo.dev%2Fblog">The Go Blog</a></h2>
  <a class="result__snippet">Official updates and articles from the Go team.</a>
</div>
</body></html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(mockHTML))
	}))
	defer server.Close()

	provider := NewDuckDuckGoSearchProvider().
		WithBaseURL(server.URL).
		WithHTTPClient(server.Client())

	if provider.Name() != "duckduckgo" {
		t.Errorf("expected provider name duckduckgo, got %s", provider.Name())
	}

	results, err := provider.Search(context.Background(), "golang", 5)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Title != "Go Packages" {
		t.Errorf("expected title 'Go Packages', got %s", results[0].Title)
	}
	if results[0].URL != "https://golang.org/pkg" {
		t.Errorf("expected URL https://golang.org/pkg, got %s", results[0].URL)
	}

	// Empty query test
	emptyRes, err := provider.Search(context.Background(), "  ", 5)
	if err != nil || len(emptyRes) != 0 {
		t.Errorf("expected empty results on whitespace query, got %v, err: %v", emptyRes, err)
	}

	// Nil receiver safety
	var nilDDG *DuckDuckGoSearchProvider = nil
	if _, err := nilDDG.Search(context.Background(), "test", 5); err == nil {
		t.Error("expected error on nil DDG search provider")
	}
}

func TestBraveSearchProvider_Search(t *testing.T) {
	var capturedToken string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedToken = r.Header.Get("X-Subscription-Token")
		w.Header().Set("Content-Type", "application/json")
		resp := braveSearchResponse{}
		resp.Web.Results = []braveWebResult{
			{
				Title:       "Brave Search Result 1",
				URL:         "https://example.com/brave1",
				Description: "Snippet from Brave search 1.",
			},
			{
				Title:       "Brave Search Result 2",
				URL:         "https://example.com/brave2",
				Description: "Snippet from Brave search 2.",
			},
			{
				Title:       "Media Image Resource",
				URL:         "https://example.com/image.png",
				Description: "Should be filtered out.",
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewBraveSearchProvider("test-brave-token").
		WithBaseURL(server.URL).
		WithHTTPClient(server.Client())

	if provider.Name() != "brave" {
		t.Errorf("expected provider name brave, got %s", provider.Name())
	}

	results, err := provider.Search(context.Background(), "concurrency in rust", 5)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if capturedToken != "test-brave-token" {
		t.Errorf("expected X-Subscription-Token 'test-brave-token', got '%s'", capturedToken)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results (image filtered out), got %d", len(results))
	}
	if results[0].Title != "Brave Search Result 1" {
		t.Errorf("expected title 'Brave Search Result 1', got %s", results[0].Title)
	}
	if results[0].URL != "https://example.com/brave1" {
		t.Errorf("expected URL https://example.com/brave1, got %s", results[0].URL)
	}

	// Nil receiver safety
	var nilBrave *BraveSearchProvider = nil
	if _, err := nilBrave.Search(context.Background(), "test", 5); err == nil {
		t.Error("expected error on nil Brave search provider")
	}
}

func TestSearXNGSearchProvider_Search(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := searXNGResponse{
			Results: []searXNGResultItem{
				{
					Title:   "SearXNG Item 1",
					URL:     "https://example.com/searx1",
					Content: "SearXNG content 1",
				},
				{
					Title:   "SearXNG Item 2",
					URL:     "https://example.com/searx2",
					Content: "SearXNG content 2",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewSearXNGSearchProvider(server.URL).
		WithHTTPClient(server.Client())

	if provider.Name() != "searxng" {
		t.Errorf("expected provider name searxng, got %s", provider.Name())
	}

	results, err := provider.Search(context.Background(), "distributed systems", 5)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Title != "SearXNG Item 1" {
		t.Errorf("expected title 'SearXNG Item 1', got %s", results[0].Title)
	}

	// Nil receiver safety
	var nilSearx *SearXNGSearchProvider = nil
	if _, err := nilSearx.Search(context.Background(), "test", 5); err == nil {
		t.Error("expected error on nil SearXNG search provider")
	}
}

func TestNewSearchProviderFromEnv(t *testing.T) {
	os.Setenv("SEARCH_PROVIDER", "brave")
	os.Setenv("BRAVE_API_KEY", "test-key")
	p1 := NewSearchProviderFromEnv()
	if p1.Name() != "brave" {
		t.Errorf("expected brave provider, got %s", p1.Name())
	}

	os.Setenv("SEARCH_PROVIDER", "searxng")
	p2 := NewSearchProviderFromEnv()
	if p2.Name() != "searxng" {
		t.Errorf("expected searxng provider, got %s", p2.Name())
	}

	os.Unsetenv("SEARCH_PROVIDER")
	os.Unsetenv("METASEARCH_PROVIDER")
	os.Unsetenv("BRAVE_API_KEY")
	p3 := NewSearchProviderFromEnv()
	if p3.Name() != "duckduckgo" {
		t.Errorf("expected duckduckgo provider, got %s", p3.Name())
	}
}

type mockCountingProvider struct {
	name         string
	requestCount int32
	results      []SearchResult
}

func (m *mockCountingProvider) Name() string {
	return m.name
}

func (m *mockCountingProvider) Search(ctx context.Context, query string, topK int) ([]SearchResult, error) {
	atomic.AddInt32(&m.requestCount, 1)
	time.Sleep(20 * time.Millisecond)
	return m.results, nil
}

func TestMetasearchAdapter_ProviderAwareSingleflight(t *testing.T) {
	eng := index.NewIndexEngine()

	mockP1 := &mockCountingProvider{
		name: "provider_alpha",
		results: []SearchResult{
			{Title: "Alpha Doc", URL: "https://example.com/alpha", Snippet: "Alpha snippet"},
		},
	}

	mockP2 := &mockCountingProvider{
		name: "provider_beta",
		results: []SearchResult{
			{Title: "Beta Doc", URL: "https://example.com/beta", Snippet: "Beta snippet"},
		},
	}

	adapter1 := NewMetasearchAdapter(eng).WithProvider(mockP1)
	adapter2 := NewMetasearchAdapter(eng).WithProvider(mockP2)

	var wg sync.WaitGroup
	wg.Add(4)

	// Two concurrent searches on adapter1 (provider_alpha)
	go func() {
		defer wg.Done()
		_, _ = adapter1.Search(context.Background(), "shared query", 5)
	}()
	go func() {
		defer wg.Done()
		_, _ = adapter1.Search(context.Background(), "shared query", 5)
	}()

	// Two concurrent searches on adapter2 (provider_beta)
	go func() {
		defer wg.Done()
		_, _ = adapter2.Search(context.Background(), "shared query", 5)
	}()
	go func() {
		defer wg.Done()
		_, _ = adapter2.Search(context.Background(), "shared query", 5)
	}()

	wg.Wait()

	// Provider Alpha should have received exactly 1 singleflight call
	if count := atomic.LoadInt32(&mockP1.requestCount); count != 1 {
		t.Errorf("expected provider_alpha to deduplicate to 1 request, got %d", count)
	}

	// Provider Beta should have received exactly 1 singleflight call
	if count := atomic.LoadInt32(&mockP2.requestCount); count != 1 {
		t.Errorf("expected provider_beta to deduplicate to 1 request, got %d", count)
	}
}
