package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crawler-monorepo/common/robotstxt"
)

func TestHealthEndpoint(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewEmbeddedServer(tmpDir)
	router := server.SetupRouter()

	req, err := http.NewRequest("GET", "/health", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP status 200, got: %d", rec.Code)
	}

	expectedBody := `{"status":"ok","mode":"single_binary_embedded"}`
	if strings.TrimSpace(rec.Body.String()) != expectedBody {
		t.Fatalf("expected body %s, got: %s", expectedBody, rec.Body.String())
	}
}

type mockTransport struct{}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	htmlBody := `<!DOCTYPE html><html><head><title>AgentLimbs Embedded Document</title></head><body><h1>AgentLimbs Embedded Vector Search</h1><p>AgentLimbs is a high performance single binary embedded web crawler and search server.</p></body></html>`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(htmlBody)),
		Header:     make(http.Header),
		Request:    req,
	}
	resp.Header.Set("Content-Type", "text/html; charset=utf-8")
	return resp, nil
}

func TestScrapeAndSearchEndpoints(t *testing.T) {
	testURL := "https://example.com/embedded-test-page"
	robotstxt.GlobalDomainCache.FetchAndCache("example.com", "User-agent: *\nAllow: /\n")

	// 1. Setup embedded server with mock transport
	tmpDir := t.TempDir()
	server := NewEmbeddedServer(tmpDir)
	server.HTTPClient().SetTransport(&mockTransport{})
	router := server.SetupRouter()

	// 2. Test POST /v1/scrape
	scrapeReqBody, _ := json.Marshal(map[string]interface{}{
		"url":  testURL,
		"mode": "clean_rag",
	})
	scrapeReq, err := http.NewRequest("POST", "/v1/scrape", bytes.NewBuffer(scrapeReqBody))
	if err != nil {
		t.Fatalf("failed to create scrape request: %v", err)
	}
	scrapeReq.Header.Set("Content-Type", "application/json")

	recScrape := httptest.NewRecorder()
	router.ServeHTTP(recScrape, scrapeReq)

	if recScrape.Code != http.StatusOK {
		t.Fatalf("expected scrape HTTP 200, got %d: %s", recScrape.Code, recScrape.Body.String())
	}

	var scrapeResp ScrapeResponse
	if err := json.Unmarshal(recScrape.Body.Bytes(), &scrapeResp); err != nil {
		t.Fatalf("failed to parse scrape response: %v", err)
	}

	if scrapeResp.Title != "AgentLimbs Embedded Document" {
		t.Errorf("expected title 'AgentLimbs Embedded Document', got: %s", scrapeResp.Title)
	}
	if !strings.Contains(scrapeResp.Markdown, "AgentLimbs") {
		t.Errorf("expected markdown content to contain 'AgentLimbs', got: %s", scrapeResp.Markdown)
	}

	// 3. Test POST /v1/search
	searchReqBody, _ := json.Marshal(map[string]interface{}{
		"query": "embedded web crawler search server",
		"top_k": 5,
	})
	searchReq, err := http.NewRequest("POST", "/v1/search", bytes.NewBuffer(searchReqBody))
	if err != nil {
		t.Fatalf("failed to create search request: %v", err)
	}
	searchReq.Header.Set("Content-Type", "application/json")

	recSearch := httptest.NewRecorder()
	router.ServeHTTP(recSearch, searchReq)

	if recSearch.Code != http.StatusOK {
		t.Fatalf("expected search HTTP 200, got %d: %s", recSearch.Code, recSearch.Body.String())
	}

	var searchResp SearchResponse
	if err := json.Unmarshal(recSearch.Body.Bytes(), &searchResp); err != nil {
		t.Fatalf("failed to parse search response: %v", err)
	}

	if searchResp.Query != "embedded web crawler search server" {
		t.Errorf("expected query 'embedded web crawler search server', got: %s", searchResp.Query)
	}
	if searchResp.TotalHits == 0 {
		t.Errorf("expected at least 1 search hit, got 0")
	}
}

func TestStorageInitAndSave(t *testing.T) {
	tmpDir := t.TempDir()
	saveStorage(tmpDir)

	indexPath := filepath.Join(tmpDir, "inverted_index.json")
	vectorPath := filepath.Join(tmpDir, "vector_index.json")

	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		t.Errorf("expected inverted index snapshot file at %s", indexPath)
	}
	if _, err := os.Stat(vectorPath); os.IsNotExist(err) {
		t.Errorf("expected vector index snapshot file at %s", vectorPath)
	}

	// Test initStorage loads without crashing
	initStorage(tmpDir)
}
