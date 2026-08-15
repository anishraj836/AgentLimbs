package index

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestSubwordEmbedder_EmbedBatch(t *testing.T) {
	emb := NewSubwordEmbedder(64)

	texts := []string{
		"Distributed systems in Go",
		"Inverted indexing and BM25 ranking",
		"Vector search with Cosine Similarity",
	}

	ctx := context.Background()
	results, err := emb.EmbedBatch(ctx, texts)
	if err != nil {
		t.Fatalf("EmbedBatch failed: %v", err)
	}

	if len(results) != len(texts) {
		t.Fatalf("expected %d results, got %d", len(texts), len(results))
	}

	for i, vec := range results {
		if len(vec) != 64 {
			t.Errorf("expected vector %d to have dimension 64, got %d", i, len(vec))
		}
	}

	// Empty slice test
	emptyRes, err := emb.EmbedBatch(ctx, []string{})
	if err != nil || len(emptyRes) != 0 {
		t.Errorf("expected empty results on empty input, got %v, err: %v", emptyRes, err)
	}

	// Context cancellation test
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = emb.EmbedBatch(cancelledCtx, texts)
	if err == nil {
		t.Error("expected error on pre-cancelled context, got nil")
	}

	// Nil receiver test
	var nilEmb *SubwordEmbedder = nil
	if _, err := nilEmb.EmbedBatch(ctx, texts); err == nil {
		t.Error("expected error on nil receiver EmbedBatch")
	}
}

func TestCohereEmbedder_EmbedBatch_Chunking(t *testing.T) {
	var requestCount int32
	var maxChunkSize int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)

		if auth := r.Header.Get("Authorization"); auth != "Bearer test-cohere-key" {
			t.Errorf("expected Authorization header 'Bearer test-cohere-key', got '%s'", auth)
		}

		var req cohereRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		chunkLen := int32(len(req.Texts))
		for {
			curMax := atomic.LoadInt32(&maxChunkSize)
			if chunkLen <= curMax || atomic.CompareAndSwapInt32(&maxChunkSize, curMax, chunkLen) {
				break
			}
		}

		embeddings := make([][]float64, len(req.Texts))
		for i := range req.Texts {
			embeddings[i] = make([]float64, 1024)
			embeddings[i][0] = float64(i + 1)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(cohereResponse{Embeddings: embeddings})
	}))
	defer server.Close()

	emb := NewCohereEmbedder("test-cohere-key").
		WithBaseURL(server.URL).
		WithHTTPClient(server.Client())

	// Create 150 texts (exceeds Cohere chunk limit of 96)
	texts := make([]string, 150)
	for i := 0; i < 150; i++ {
		texts[i] = fmt.Sprintf("Sample document passage %d", i+1)
	}

	ctx := context.Background()
	results, err := emb.EmbedBatch(ctx, texts)
	if err != nil {
		t.Fatalf("EmbedBatch failed: %v", err)
	}

	if len(results) != 150 {
		t.Fatalf("expected 150 results, got %d", len(results))
	}

	// 150 items should be split into 2 chunks (96 + 54)
	if count := atomic.LoadInt32(&requestCount); count != 2 {
		t.Errorf("expected 2 chunked requests, got %d", count)
	}

	if maxChunk := atomic.LoadInt32(&maxChunkSize); maxChunk > 96 {
		t.Errorf("expected max chunk size <= 96, got %d", maxChunk)
	}

	// Single embed test
	singleVec, err := emb.Embed(ctx, "Single passage")
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	if len(singleVec) != 1024 {
		t.Errorf("expected 1024 dims, got %d", len(singleVec))
	}

	// Nil receiver safety
	var nilCohere *CohereEmbedder = nil
	if nilCohere.Dimensions() != 0 {
		t.Errorf("expected 0 dims on nil Cohere, got %d", nilCohere.Dimensions())
	}
	if _, err := nilCohere.Embed(ctx, "test"); err == nil {
		t.Error("expected error on nil Cohere Embed")
	}
	if _, err := nilCohere.EmbedBatch(ctx, []string{"test"}); err == nil {
		t.Error("expected error on nil Cohere EmbedBatch")
	}
}

func TestOpenAIEmbedder_EmbedBatch_Chunking(t *testing.T) {
	var requestCount int32
	var maxChunkSize int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)

		if auth := r.Header.Get("Authorization"); auth != "Bearer test-openai-key" {
			t.Errorf("expected Authorization header 'Bearer test-openai-key', got '%s'", auth)
		}

		var req struct {
			Input []string `json:"input"`
			Model string   `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		chunkLen := int32(len(req.Input))
		for {
			curMax := atomic.LoadInt32(&maxChunkSize)
			if chunkLen <= curMax || atomic.CompareAndSwapInt32(&maxChunkSize, curMax, chunkLen) {
				break
			}
		}

		items := make([]openAIEmbedItem, len(req.Input))
		for i := range req.Input {
			vec := make([]float64, 1536)
			vec[0] = float64(i + 1)
			items[i] = openAIEmbedItem{
				Index:     i,
				Embedding: vec,
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(openAIEmbedResponse{Data: items})
	}))
	defer server.Close()

	emb := NewOpenAIEmbedder("test-openai-key").
		WithBaseURL(server.URL).
		WithHTTPClient(server.Client())

	// Create 250 texts (exceeds OpenAI chunk limit of 200)
	texts := make([]string, 250)
	for i := 0; i < 250; i++ {
		texts[i] = fmt.Sprintf("OpenAI embedding document text %d", i+1)
	}

	ctx := context.Background()
	results, err := emb.EmbedBatch(ctx, texts)
	if err != nil {
		t.Fatalf("EmbedBatch failed: %v", err)
	}

	if len(results) != 250 {
		t.Fatalf("expected 250 results, got %d", len(results))
	}

	// 250 items should be split into 2 chunks (200 + 50)
	if count := atomic.LoadInt32(&requestCount); count != 2 {
		t.Errorf("expected 2 chunked requests, got %d", count)
	}

	if maxChunk := atomic.LoadInt32(&maxChunkSize); maxChunk > 200 {
		t.Errorf("expected max chunk size <= 200, got %d", maxChunk)
	}

	// Single embed test
	singleVec, err := emb.Embed(ctx, "Single text")
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	if len(singleVec) != 1536 {
		t.Errorf("expected 1536 dims, got %d", len(singleVec))
	}

	// Nil receiver safety
	var nilOpenAI *OpenAIEmbedder = nil
	if nilOpenAI.Dimensions() != 0 {
		t.Errorf("expected 0 dims, got %d", nilOpenAI.Dimensions())
	}
	if _, err := nilOpenAI.Embed(ctx, "test"); err == nil {
		t.Error("expected error on nil OpenAI Embed")
	}
	if _, err := nilOpenAI.EmbedBatch(ctx, []string{"test"}); err == nil {
		t.Error("expected error on nil OpenAI EmbedBatch")
	}
}

func TestOllamaEmbedder_EmbedBatch(t *testing.T) {
	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)

		var req ollamaRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		vec := make([]float64, 768)
		vec[0] = 0.42

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ollamaResponse{Embedding: vec})
	}))
	defer server.Close()

	emb := NewOllamaEmbedder().
		WithHost(server.URL).
		WithHTTPClient(server.Client())

	texts := []string{
		"Ollama doc 1",
		"Ollama doc 2",
		"Ollama doc 3",
	}

	ctx := context.Background()
	results, err := emb.EmbedBatch(ctx, texts)
	if err != nil {
		t.Fatalf("EmbedBatch failed: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	if count := atomic.LoadInt32(&requestCount); count != 3 {
		t.Errorf("expected 3 requests to Ollama endpoint, got %d", count)
	}

	// Single embed test
	singleVec, err := emb.Embed(ctx, "Single text")
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	if len(singleVec) != 768 {
		t.Errorf("expected 768 dims, got %d", len(singleVec))
	}

	// Context cancellation check
	cancelCtx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	time.Sleep(5 * time.Millisecond)
	_, err = emb.EmbedBatch(cancelCtx, texts)
	if err == nil {
		t.Error("expected error on cancelled context")
	}

	// Nil receiver safety
	var nilOllama *OllamaEmbedder = nil
	if nilOllama.Dimensions() != 0 {
		t.Errorf("expected 0 dims, got %d", nilOllama.Dimensions())
	}
	if _, err := nilOllama.Embed(ctx, "test"); err == nil {
		t.Error("expected error on nil Ollama Embed")
	}
	if _, err := nilOllama.EmbedBatch(ctx, []string{"test"}); err == nil {
		t.Error("expected error on nil Ollama EmbedBatch")
	}
}
