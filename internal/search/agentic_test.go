package search

import (
	"context"
	"encoding/json"
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

func TestAgenticPipeline_ContextCancellation(t *testing.T) {
	eng := index.NewIndexEngine()
	pipeline := NewAgenticPipeline(eng)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Pre-cancel context

	_, err := pipeline.Execute(ctx, AgenticSearchRequest{
		Query: "test query",
	})
	if err == nil {
		t.Fatalf("Expected context error from Execute when context is cancelled, got nil")
	}
	if err != context.Canceled {
		t.Errorf("Expected context.Canceled, got %v", err)
	}
}

func TestAgenticPipeline_XMLContextEncapsulation(t *testing.T) {
	var capturedUserPrompt, capturedSystemPrompt string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&reqBody)
		for _, m := range reqBody.Messages {
			if m.Role == "system" {
				capturedSystemPrompt = m.Content
			} else if m.Role == "user" {
				capturedUserPrompt = m.Content
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Verified output."}}]}`))
	}))
	defer server.Close()

	eng := index.NewIndexEngine()
	pipeline := NewAgenticPipeline(eng)
	pipeline.httpClient = server.Client()
	pipeline.allowLoopbackForTesting = true

	hits := []HybridSearchHit{
		{DocID: "d1", Title: "Title 1", URL: "https://example.com/1", Snippet: "Snippet 1"},
	}

	_, err := pipeline.callDeepSeek(context.Background(), server.URL, "key", "model", "query", hits)
	if err != nil {
		t.Fatalf("callDeepSeek failed: %v", err)
	}

	if !strings.Contains(capturedUserPrompt, "<context>\n[1] Title: Title 1") {
		t.Errorf("Expected <context> tag wrapping in user prompt, got: %s", capturedUserPrompt)
	}
	if !strings.Contains(capturedUserPrompt, "</context>") {
		t.Errorf("Expected </context> closing tag in user prompt")
	}
	if !strings.Contains(capturedSystemPrompt, "untrusted reference data") {
		t.Errorf("Expected untrusted reference data instruction in system prompt, got: %s", capturedSystemPrompt)
	}
}

func TestAgenticPipeline_CheckRedirectSSRF(t *testing.T) {
	eng := index.NewIndexEngine()
	pipeline := NewAgenticPipeline(eng) // allowLoopback = false

	if pipeline.httpClient.CheckRedirect == nil {
		t.Fatalf("Expected CheckRedirect hook to be configured on pipeline httpClient")
	}

	req, _ := http.NewRequest("GET", "http://127.0.0.1/admin", nil)
	via := []*http.Request{req}
	err := pipeline.httpClient.CheckRedirect(req, via)
	if err == nil || !strings.Contains(err.Error(), "blocked redirect to private IP") {
		t.Errorf("Expected CheckRedirect to block redirect to 127.0.0.1, got: %v", err)
	}
}
