package index

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/crawler-monorepo/common/logger"
	"go.uber.org/zap"
)

// Embedder generates dense vector embeddings from text.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float64, error)
	Dimensions() int
	ProviderName() string
}

// SubwordEmbedder produces feature vectors using subword n-gram hashing.
type SubwordEmbedder struct {
	dimensions int
}

// NewSubwordEmbedder returns a SubwordEmbedder with the given vector dimensions.
func NewSubwordEmbedder(dimensions int) *SubwordEmbedder {
	if dimensions <= 0 {
		dimensions = 128
	}
	return &SubwordEmbedder{dimensions: dimensions}
}

func (s *SubwordEmbedder) Dimensions() int {
	return s.dimensions
}

func (s *SubwordEmbedder) ProviderName() string {
	return "subword_n_gram"
}

func (s *SubwordEmbedder) Embed(ctx context.Context, text string) ([]float64, error) {
	return GenerateFeatureVector(text, s.dimensions), nil
}

// CohereEmbedder generates embeddings using the Cohere API.
type CohereEmbedder struct {
	apiKey     string
	model      string
	dimensions int
	client     *http.Client
}

// NewCohereEmbedder returns a CohereEmbedder configured with the provided API key.
func NewCohereEmbedder(apiKey string) *CohereEmbedder {
	model := os.Getenv("COHERE_MODEL")
	if model == "" {
		model = "embed-english-v3.0"
	}
	return &CohereEmbedder{
		apiKey:     apiKey,
		model:      model,
		dimensions: 1024,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *CohereEmbedder) Dimensions() int {
	return c.dimensions
}

func (c *CohereEmbedder) ProviderName() string {
	return "cohere"
}

type cohereRequest struct {
	Texts     []string `json:"texts"`
	Model     string   `json:"model"`
	InputType string   `json:"input_type"`
}

type cohereResponse struct {
	Embeddings [][]float64 `json:"embeddings"`
}

func (c *CohereEmbedder) Embed(ctx context.Context, text string) ([]float64, error) {
	reqBody, _ := json.Marshal(cohereRequest{
		Texts:     []string{text},
		Model:     c.model,
		InputType: "search_document",
	})

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.cohere.com/v1/embed", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("cohere api error (status %d): %s", resp.StatusCode, string(body))
	}

	var res cohereResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	if len(res.Embeddings) == 0 {
		return nil, fmt.Errorf("cohere returned empty embeddings")
	}

	return res.Embeddings[0], nil
}

// OpenAIEmbedder generates embeddings using the OpenAI API.
type OpenAIEmbedder struct {
	apiKey     string
	model      string
	dimensions int
	client     *http.Client
}

// NewOpenAIEmbedder returns an OpenAIEmbedder configured with the provided API key.
func NewOpenAIEmbedder(apiKey string) *OpenAIEmbedder {
	model := os.Getenv("OPENAI_MODEL")
	if model == "" {
		model = "text-embedding-3-small"
	}
	dims := 1536
	if envDims := os.Getenv("OPENAI_EMBED_DIMENSIONS"); envDims != "" {
		if parsed, err := strconv.Atoi(envDims); err == nil && parsed > 0 {
			dims = parsed
		}
	}
	return &OpenAIEmbedder{
		apiKey:     apiKey,
		model:      model,
		dimensions: dims,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (o *OpenAIEmbedder) Dimensions() int {
	return o.dimensions
}

func (o *OpenAIEmbedder) ProviderName() string {
	return "openai"
}

type openAIRequest struct {
	Input string `json:"input"`
	Model string `json:"model"`
}

type openAIResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
}

func (o *OpenAIEmbedder) Embed(ctx context.Context, text string) ([]float64, error) {
	reqBody, _ := json.Marshal(openAIRequest{
		Input: text,
		Model: o.model,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/embeddings", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+o.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai api error (status %d): %s", resp.StatusCode, string(body))
	}

	var res openAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	if len(res.Data) == 0 {
		return nil, fmt.Errorf("openai returned empty embeddings")
	}

	return res.Data[0].Embedding, nil
}

// OllamaEmbedder generates embeddings using a local Ollama service.
type OllamaEmbedder struct {
	host       string
	model      string
	dimensions int
	client     *http.Client
}

// NewOllamaEmbedder returns an OllamaEmbedder configured via environment variables.
func NewOllamaEmbedder() *OllamaEmbedder {
	host := os.Getenv("OLLAMA_HOST")
	if host == "" {
		host = "http://localhost:11434"
	}
	model := os.Getenv("OLLAMA_MODEL")
	if model == "" {
		model = "nomic-embed-text"
	}
	dims := 768
	if envDims := os.Getenv("OLLAMA_EMBED_DIMENSIONS"); envDims != "" {
		if parsed, err := strconv.Atoi(envDims); err == nil && parsed > 0 {
			dims = parsed
		}
	}
	return &OllamaEmbedder{
		host:       host,
		model:      model,
		dimensions: dims,
		client:     &http.Client{Timeout: 15 * time.Second},
	}
}

func (ol *OllamaEmbedder) Dimensions() int {
	return ol.dimensions
}

func (ol *OllamaEmbedder) ProviderName() string {
	return "ollama"
}

type ollamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type ollamaResponse struct {
	Embedding []float64 `json:"embedding"`
}

func (ol *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float64, error) {
	reqBody, _ := json.Marshal(ollamaRequest{
		Model:  ol.model,
		Prompt: text,
	})

	apiURL := strings.TrimRight(ol.host, "/") + "/api/embeddings"
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ol.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama api error (status %d): %s", resp.StatusCode, string(body))
	}

	var res ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	if len(res.Embedding) == 0 {
		return nil, fmt.Errorf("ollama returned empty embeddings")
	}

	return res.Embedding, nil
}

// NewEmbedderFromEnv initializes an Embedder based on the EMBEDDING_PROVIDER environment variable.
func NewEmbedderFromEnv() Embedder {
	provider := strings.ToLower(os.Getenv("EMBEDDING_PROVIDER"))
	switch provider {
	case "cohere":
		key := os.Getenv("COHERE_API_KEY")
		if key != "" {
			logger.Log.Info("Initialized Cohere Embedder provider")
			return NewCohereEmbedder(key)
		}
		logger.Log.Warn("COHERE_API_KEY missing, falling back to subword embedder", zap.String("provider", provider))
	case "openai":
		key := os.Getenv("OPENAI_API_KEY")
		if key != "" {
			logger.Log.Info("Initialized OpenAI Embedder provider")
			return NewOpenAIEmbedder(key)
		}
		logger.Log.Warn("OPENAI_API_KEY missing, falling back to subword embedder", zap.String("provider", provider))
	case "ollama":
		logger.Log.Info("Initialized Ollama Local Embedder provider")
		return NewOllamaEmbedder()
	}

	return NewSubwordEmbedder(128)
}
