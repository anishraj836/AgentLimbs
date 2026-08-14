package search

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/crawler-monorepo/internal/index"
)

func TestValidateLLMBaseURL(t *testing.T) {
	blockedURLs := []string{
		"http://127.0.0.1:8080",
		"http://127.0.0.1:8080/v1",
		"http://10.0.0.1/v1",
		"http://172.16.0.1/v1",
		"http://192.168.1.1/v1",
		"http://169.254.169.254/latest/meta-data",
		"http://0.0.0.0:8000",
		"http://localhost:8080/v1",
		"ftp://api.deepseek.com/v1",
		"javascript:alert(1)",
		"://bad-url",
	}

	for _, u := range blockedURLs {
		err := validateLLMBaseURL(u)
		if err == nil {
			t.Errorf("Expected validateLLMBaseURL(%q) to fail with SSRF error, but it succeeded", u)
		}
	}

	allowedURLs := []string{
		"https://api.deepseek.com/v1",
		"https://api.openai.com/v1",
		"http://example.com/v1",
	}

	for _, u := range allowedURLs {
		err := validateLLMBaseURL(u)
		if err != nil {
			t.Errorf("Expected validateLLMBaseURL(%q) to succeed, but got: %v", u, err)
		}
	}
}

func TestAgenticPipeline_SSRFRejection(t *testing.T) {
	eng := index.NewIndexEngine()
	pipeline := NewAgenticPipeline(eng)

	req := AgenticSearchRequest{
		Query:      "test ssrf query",
		LLMBaseURL: "http://127.0.0.1:8080/v1",
		LLMApiKey:  "test-api-key",
	}

	_, err := pipeline.Execute(context.Background(), req)
	if err == nil || !strings.Contains(err.Error(), "invalid LLM base URL") {
		t.Fatalf("Expected Execute to reject SSRF base URL, got err: %v", err)
	}
}

func TestAgenticPipeline_CallDeepSeek_SocketDraining(t *testing.T) {
	var handledCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handledCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"choices": [
				{
					"message": {
						"content": "Synthesized response from mock LLM."
					}
				}
			]
		}`))
	}))
	defer server.Close()

	eng := index.NewIndexEngine()
	pipeline := NewAgenticPipeline(eng)
	pipeline.httpClient = server.Client()
	pipeline.allowLoopbackForTesting = true

	hits := []HybridSearchHit{
		{
			DocID:   "doc1",
			Title:   "Doc 1",
			URL:     "https://example.com/doc1",
			Snippet: "This is snippet 1",
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ans, err := pipeline.callDeepSeek(ctx, server.URL, "mock-key", "test-model", "test query", hits)
	if err != nil {
		t.Fatalf("callDeepSeek failed: %v", err)
	}
	if ans != "Synthesized response from mock LLM." {
		t.Errorf("Unexpected synthesized response: %q", ans)
	}
}
