package search

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/crawler-monorepo/common/logger"
	"github.com/crawler-monorepo/internal/crawler"
)

// SearchResult represents a unified search hit returned by external search providers.
type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// SearchProvider defines the interface for external web search engine integrations.
type SearchProvider interface {
	Name() string
	Search(ctx context.Context, query string, topK int) ([]SearchResult, error)
}

// Compile-time interface compliance assertions
var (
	_ SearchProvider = (*DuckDuckGoSearchProvider)(nil)
	_ SearchProvider = (*BraveSearchProvider)(nil)
	_ SearchProvider = (*SearXNGSearchProvider)(nil)
)

// -----------------------------------------------------------------------------
// DuckDuckGo Search Provider
// -----------------------------------------------------------------------------

// DuckDuckGoSearchProvider scrapes HTML search results from DuckDuckGo.
type DuckDuckGoSearchProvider struct {
	baseURL    string
	httpClient *http.Client
}

// NewDuckDuckGoSearchProvider creates a DuckDuckGo search provider.
func NewDuckDuckGoSearchProvider() *DuckDuckGoSearchProvider {
	baseURL := os.Getenv("DDG_BASE_URL")
	if baseURL == "" {
		baseURL = "https://html.duckduckgo.com/html/"
	}
	return &DuckDuckGoSearchProvider{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

func (d *DuckDuckGoSearchProvider) WithBaseURL(baseURL string) *DuckDuckGoSearchProvider {
	if d != nil {
		d.baseURL = baseURL
	}
	return d
}

func (d *DuckDuckGoSearchProvider) WithHTTPClient(client *http.Client) *DuckDuckGoSearchProvider {
	if d != nil {
		d.httpClient = client
	}
	return d
}

func (d *DuckDuckGoSearchProvider) Name() string {
	return "duckduckgo"
}

func (d *DuckDuckGoSearchProvider) Search(ctx context.Context, query string, topK int) ([]SearchResult, error) {
	if d == nil {
		return nil, fmt.Errorf("duckduckgo search provider is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	query = strings.TrimSpace(query)
	if query == "" {
		return []SearchResult{}, nil
	}

	reqURL := d.baseURL
	if strings.Contains(reqURL, "?") {
		reqURL += "&q=" + url.QueryEscape(query)
	} else {
		reqURL += "?q=" + url.QueryEscape(query)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}

	profile := crawler.GetRotatedHeaderProfile()
	crawler.ApplyAntiBotHeaders(req, profile)

	client := d.httpClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024*1024))
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DuckDuckGo endpoint returned HTTP status %d", resp.StatusCode)
	}

	limited := io.LimitReader(resp.Body, 10*1024*1024)
	htmlBytes, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}

	results, err := ParseDDGHTML(htmlBytes)
	if err != nil {
		return nil, err
	}

	if topK > 0 && len(results) > topK {
		results = results[:topK]
	}

	return results, nil
}

// -----------------------------------------------------------------------------
// Brave Search Provider
// -----------------------------------------------------------------------------

// BraveSearchProvider integrates with the official Brave Search API.
type BraveSearchProvider struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// NewBraveSearchProvider creates a Brave search provider with the given API key.
func NewBraveSearchProvider(apiKey string) *BraveSearchProvider {
	baseURL := os.Getenv("BRAVE_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.search.brave.com/res/v1/web/search"
	}
	return &BraveSearchProvider{
		apiKey:     apiKey,
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

func (b *BraveSearchProvider) WithBaseURL(baseURL string) *BraveSearchProvider {
	if b != nil {
		b.baseURL = baseURL
	}
	return b
}

func (b *BraveSearchProvider) WithHTTPClient(client *http.Client) *BraveSearchProvider {
	if b != nil {
		b.httpClient = client
	}
	return b
}

func (b *BraveSearchProvider) WithAPIKey(key string) *BraveSearchProvider {
	if b != nil {
		b.apiKey = key
	}
	return b
}

func (b *BraveSearchProvider) Name() string {
	return "brave"
}

type braveWebResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
}

type braveSearchResponse struct {
	Web struct {
		Results []braveWebResult `json:"results"`
	} `json:"web"`
}

func (b *BraveSearchProvider) Search(ctx context.Context, query string, topK int) ([]SearchResult, error) {
	if b == nil {
		return nil, fmt.Errorf("brave search provider is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	query = strings.TrimSpace(query)
	if query == "" {
		return []SearchResult{}, nil
	}

	if topK <= 0 {
		topK = 5
	}

	reqURL := fmt.Sprintf("%s?q=%s&count=%d", b.baseURL, url.QueryEscape(query), topK)
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
	if b.apiKey != "" {
		req.Header.Set("X-Subscription-Token", b.apiKey)
	}

	client := b.httpClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024*1024))
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("Brave Search API returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var res braveSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	var results []SearchResult
	for _, item := range res.Web.Results {
		if item.URL == "" || crawler.IsMediaResourceURL(item.URL) {
			continue
		}
		title := strings.TrimSpace(item.Title)
		if title == "" {
			title = item.URL
		}
		results = append(results, SearchResult{
			Title:   title,
			URL:     item.URL,
			Snippet: strings.TrimSpace(item.Description),
		})
	}

	if topK > 0 && len(results) > topK {
		results = results[:topK]
	}

	return results, nil
}

// -----------------------------------------------------------------------------
// SearXNG Search Provider
// -----------------------------------------------------------------------------

// SearXNGSearchProvider integrates with SearXNG self-hosted metasearch instance.
type SearXNGSearchProvider struct {
	baseURL    string
	httpClient *http.Client
}

// NewSearXNGSearchProvider creates a SearXNG search provider.
func NewSearXNGSearchProvider(baseURL string) *SearXNGSearchProvider {
	if baseURL == "" {
		baseURL = os.Getenv("SEARXNG_URL")
	}
	if baseURL == "" {
		baseURL = os.Getenv("SEARXNG_BASE_URL")
	}
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	baseURL = strings.TrimRight(baseURL, "/")

	return &SearXNGSearchProvider{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

func (s *SearXNGSearchProvider) WithBaseURL(baseURL string) *SearXNGSearchProvider {
	if s != nil {
		s.baseURL = strings.TrimRight(baseURL, "/")
	}
	return s
}

func (s *SearXNGSearchProvider) WithHTTPClient(client *http.Client) *SearXNGSearchProvider {
	if s != nil {
		s.httpClient = client
	}
	return s
}

func (s *SearXNGSearchProvider) Name() string {
	return "searxng"
}

type searXNGResultItem struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content"`
}

type searXNGResponse struct {
	Results []searXNGResultItem `json:"results"`
}

func (s *SearXNGSearchProvider) Search(ctx context.Context, query string, topK int) ([]SearchResult, error) {
	if s == nil {
		return nil, fmt.Errorf("searxng search provider is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	query = strings.TrimSpace(query)
	if query == "" {
		return []SearchResult{}, nil
	}

	if topK <= 0 {
		topK = 5
	}

	reqURL := fmt.Sprintf("%s/search?q=%s&format=json", s.baseURL, url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")

	client := s.httpClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024*1024))
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("SearXNG returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var res searXNGResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	var results []SearchResult
	for _, item := range res.Results {
		if item.URL == "" || crawler.IsMediaResourceURL(item.URL) {
			continue
		}
		title := strings.TrimSpace(item.Title)
		if title == "" {
			title = item.URL
		}
		results = append(results, SearchResult{
			Title:   title,
			URL:     item.URL,
			Snippet: strings.TrimSpace(item.Content),
		})
	}

	if topK > 0 && len(results) > topK {
		results = results[:topK]
	}

	return results, nil
}

// -----------------------------------------------------------------------------
// SearchProvider Factory Function
// -----------------------------------------------------------------------------

// NewSearchProviderFromEnv creates a SearchProvider based on SEARCH_PROVIDER or METASEARCH_PROVIDER environment variable.
func NewSearchProviderFromEnv() SearchProvider {
	provider := strings.ToLower(os.Getenv("SEARCH_PROVIDER"))
	if provider == "" {
		provider = strings.ToLower(os.Getenv("METASEARCH_PROVIDER"))
	}

	switch provider {
	case "brave":
		key := os.Getenv("BRAVE_API_KEY")
		if key != "" {
			logger.Log.Info("Initialized Brave Search Provider")
			return NewBraveSearchProvider(key)
		}
		logger.Log.Warn("BRAVE_API_KEY missing, falling back to DuckDuckGo search provider")
	case "searxng":
		baseURL := os.Getenv("SEARXNG_URL")
		logger.Log.Info("Initialized SearXNG Search Provider")
		return NewSearXNGSearchProvider(baseURL)
	}

	return NewDuckDuckGoSearchProvider()
}
