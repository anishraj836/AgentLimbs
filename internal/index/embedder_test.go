package index

import (
	"context"
	"os"
	"testing"
)

func TestSubwordEmbedder(t *testing.T) {
	embedder := NewSubwordEmbedder(128)
	if embedder.Dimensions() != 128 {
		t.Fatalf("expected 128 dimensions, got %d", embedder.Dimensions())
	}
	if embedder.ProviderName() != "subword_n_gram" {
		t.Fatalf("expected subword_n_gram provider name, got %s", embedder.ProviderName())
	}

	ctx := context.Background()
	vec, err := embedder.Embed(ctx, "Go concurrency goroutines channels")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(vec) != 128 {
		t.Fatalf("expected vector length 128, got %d", len(vec))
	}
}

func TestNewEmbedderFromEnvDefault(t *testing.T) {
	os.Unsetenv("EMBEDDING_PROVIDER")
	embedder := NewEmbedderFromEnv()

	if embedder.ProviderName() != "subword_n_gram" {
		t.Fatalf("expected default provider subword_n_gram, got %s", embedder.ProviderName())
	}
	if embedder.Dimensions() != 128 {
		t.Fatalf("expected 128 dimensions, got %d", embedder.Dimensions())
	}
}

func TestNewEmbedderFromEnvCohereFallback(t *testing.T) {
	os.Setenv("EMBEDDING_PROVIDER", "cohere")
	os.Unsetenv("COHERE_API_KEY")
	embedder := NewEmbedderFromEnv()

	// Should fallback to subword_n_gram if API key is missing
	if embedder.ProviderName() != "subword_n_gram" {
		t.Fatalf("expected fallback to subword_n_gram when COHERE_API_KEY missing, got %s", embedder.ProviderName())
	}
}

func TestNewEmbedderFromEnvOpenAIFallback(t *testing.T) {
	os.Setenv("EMBEDDING_PROVIDER", "openai")
	os.Unsetenv("OPENAI_API_KEY")
	embedder := NewEmbedderFromEnv()

	// Should fallback to subword_n_gram if API key is missing
	if embedder.ProviderName() != "subword_n_gram" {
		t.Fatalf("expected fallback to subword_n_gram when OPENAI_API_KEY missing, got %s", embedder.ProviderName())
	}
}

func TestNewEmbedderFromEnvOllama(t *testing.T) {
	os.Setenv("EMBEDDING_PROVIDER", "ollama")
	embedder := NewEmbedderFromEnv()

	if embedder.ProviderName() != "ollama" {
		t.Fatalf("expected ollama provider, got %s", embedder.ProviderName())
	}
	os.Unsetenv("EMBEDDING_PROVIDER")
}
