package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/crawler-monorepo/common/db"
	"github.com/crawler-monorepo/indexer-service/indexer"
	"github.com/crawler-monorepo/tokenizer-service/tokenizer"
)

func TestCrossProcessSearchSync(t *testing.T) {
	ctx := context.Background()

	// 1. Simulate agent-service scraping and saving document to shared DB / storage
	url := "https://example.com/golang-concurrency"
	title := "Go Concurrency Patterns"
	cleanBody := "Goroutines and channels enable high performance concurrent execution in Go programs."
	tokDoc := tokenizer.TokenizePipeline(url, title, cleanBody)

	// Save to DB / storage
	err := db.SaveCrawledDocument(ctx, tokDoc.URL, tokDoc.Title, tokDoc.CleanBody, tokDoc.TotalTokens, "web_crawled", tokDoc.URL)
	if err != nil {
		t.Fatalf("failed to save document: %v", err)
	}

	// 2. Simulate search-service periodic DB hydrator running
	searchEngine := indexer.NewIndexEngine()
	err = searchEngine.LoadFromDB(ctx)
	if err != nil {
		t.Fatalf("failed to load from DB into search-service engine: %v", err)
	}

	// 3. Query search-service directly
	handler := NewSearchHandler(searchEngine)
	reqBody := `{"query":"goroutines concurrency"}`
	req := httptest.NewRequest(http.MethodPost, "/search", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Search(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK from search-service, got %d", resp.StatusCode)
	}

	var searchResp struct {
		Query     string `json:"query"`
		TotalHits int    `json:"total_hits"`
		Results   []struct {
			URL   string  `json:"url"`
			Title string  `json:"title"`
			Score float64 `json:"score"`
		} `json:"results"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		t.Fatalf("failed to decode search-service response: %v", err)
	}

	if searchResp.TotalHits == 0 || len(searchResp.Results) == 0 {
		t.Fatalf("expected search-service to return hits for 'goroutines concurrency', got 0 hits")
	}

	if searchResp.Results[0].URL != url {
		t.Fatalf("expected top URL to be %s, got %s", url, searchResp.Results[0].URL)
	}

	t.Logf("Search-service successfully returned synced document: URL=%s, Title=%s, Score=%.4f",
		searchResp.Results[0].URL, searchResp.Results[0].Title, searchResp.Results[0].Score)
}
