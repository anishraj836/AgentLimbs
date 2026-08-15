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

// Compile-time interface compliance assertions
var (
	_ Embedder = (*SubwordEmbedder)(nil)
	_ Embedder = (*CohereEmbedder)(nil)
	_ Embedder = (*OpenAIEmbedder)(nil)
	_ Embedder = (*OllamaEmbedder)(nil)
)

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
	if s == nil {
		return 0
	}
	return s.dimensions
}

func (s *SubwordEmbedder) ProviderName() string {
	return "subword_n_gram"
}

func (s *SubwordEmbedder) Embed(ctx context.Context, text string) ([]float64, error) {
	if s == nil {
		return nil, fmt.Errorf("subword embedder is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return GenerateFeatureVector(text, s.dimensions), nil
}

func (s *SubwordEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float64, error) {
	if s == nil {
		return nil, fmt.Errorf("subword embedder is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(texts) == 0 {
		return [][]float64{}, nil
	}

	results := make([][]float64, len(texts))
	for i, text := range texts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		results[i] = GenerateFeatureVector(text, s.dimensions)
	}
	return results, nil
}

// CohereEmbedder generates embeddings using the Cohere API.
type CohereEmbedder struct {
	apiKey     string
	model      string
	dimensions int
	client     *http.Client
	baseURL    string
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
		baseURL:    "https://api.cohere.com/v1/embed",
	}
}

func (c *CohereEmbedder) WithHTTPClient(client *http.Client) *CohereEmbedder {
	if c != nil {
		c.client = client
	}
	return c
}

func (c *CohereEmbedder) WithBaseURL(url string) *CohereEmbedder {
	if c != nil {
		c.baseURL = url
	}
	return c
}

func (c *CohereEmbedder) Dimensions() int {
	if c == nil {
		return 0
	}
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
	if c == nil {
		return nil, fmt.Errorf("cohere embedder is nil")
	}
	results, err := c.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("cohere returned empty embeddings")
	}
	return results[0], nil
}

func (c *CohereEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float64, error) {
	if c == nil {
		return nil, fmt.Errorf("cohere embedder is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(texts) == 0 {
		return [][]float64{}, nil
	}

	client := c.client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	apiURL := c.baseURL
	if apiURL == "" {
		apiURL = "https://api.cohere.com/v1/embed"
	}

	const chunkSize = 96
	var allEmbeddings [][]float64

	for i := 0; i < len(texts); i += chunkSize {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := i + chunkSize
		if end > len(texts) {
			end = len(texts)
		}
		chunk := texts[i:end]

		reqBody, err := json.Marshal(cohereRequest{
			Texts:     chunk,
			Model:     c.model,
			InputType: "search_document",
		})
		if err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(reqBody))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("cohere api error (status %d): %s", resp.StatusCode, string(body))
		}

		var res cohereResponse
		err = json.NewDecoder(resp.Body).Decode(&res)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}

		if len(res.Embeddings) != len(chunk) {
			return nil, fmt.Errorf("cohere returned %d embeddings for %d inputs", len(res.Embeddings), len(chunk))
		}

		allEmbeddings = append(allEmbeddings, res.Embeddings...)
	}

	return allEmbeddings, nil
}

// OpenAIEmbedder generates embeddings using the OpenAI API.
type OpenAIEmbedder struct {
	apiKey     string
	model      string
	dimensions int
	client     *http.Client
	baseURL    string
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
		baseURL:    "https://api.openai.com/v1/embeddings",
	}
}

func (o *OpenAIEmbedder) WithHTTPClient(client *http.Client) *OpenAIEmbedder {
	if o != nil {
		o.client = client
	}
	return o
}

func (o *OpenAIEmbedder) WithBaseURL(url string) *OpenAIEmbedder {
	if o != nil {
		o.baseURL = url
	}
	return o
}

func (o *OpenAIEmbedder) Dimensions() int {
	if o == nil {
		return 0
	}
	return o.dimensions
}

func (o *OpenAIEmbedder) ProviderName() string {
	return "openai"
}

type openAIEmbedItem struct {
	Index     int       `json:"index"`
	Embedding []float64 `json:"embedding"`
}

type openAIEmbedResponse struct {
	Data []openAIEmbedItem `json:"data"`
}

func (o *OpenAIEmbedder) Embed(ctx context.Context, text string) ([]float64, error) {
	if o == nil {
		return nil, fmt.Errorf("openai embedder is nil")
	}
	results, err := o.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("openai returned empty embeddings")
	}
	return results[0], nil
}

func (o *OpenAIEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float64, error) {
	if o == nil {
		return nil, fmt.Errorf("openai embedder is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(texts) == 0 {
		return [][]float64{}, nil
	}

	client := o.client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	apiURL := o.baseURL
	if apiURL == "" {
		apiURL = "https://api.openai.com/v1/embeddings"
	}

	const chunkSize = 200
	var allEmbeddings [][]float64

	for i := 0; i < len(texts); i += chunkSize {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := i + chunkSize
		if end > len(texts) {
			end = len(texts)
		}
		chunk := texts[i:end]

		reqBody, err := json.Marshal(map[string]interface{}{
			"input": chunk,
			"model": o.model,
		})
		if err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(reqBody))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+o.apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("openai api error (status %d): %s", resp.StatusCode, string(body))
		}

		var res openAIEmbedResponse
		err = json.NewDecoder(resp.Body).Decode(&res)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}

		if len(res.Data) != len(chunk) {
			return nil, fmt.Errorf("openai returned %d embeddings for %d inputs", len(res.Data), len(chunk))
		}

		chunkEmbeddings := make([][]float64, len(chunk))
		for _, item := range res.Data {
			if item.Index >= 0 && item.Index < len(chunkEmbeddings) {
				chunkEmbeddings[item.Index] = item.Embedding
			}
		}

		allEmbeddings = append(allEmbeddings, chunkEmbeddings...)
	}

	return allEmbeddings, nil
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

func (ol *OllamaEmbedder) WithHTTPClient(client *http.Client) *OllamaEmbedder {
	if ol != nil {
		ol.client = client
	}
	return ol
}

func (ol *OllamaEmbedder) WithHost(host string) *OllamaEmbedder {
	if ol != nil {
		ol.host = host
	}
	return ol
}

func (ol *OllamaEmbedder) Dimensions() int {
	if ol == nil {
		return 0
	}
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
	if ol == nil {
		return nil, fmt.Errorf("ollama embedder is nil")
	}
	results, err := ol.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("ollama returned empty embeddings")
	}
	return results[0], nil
}

func (ol *OllamaEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float64, error) {
	if ol == nil {
		return nil, fmt.Errorf("ollama embedder is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(texts) == 0 {
		return [][]float64{}, nil
	}

	client := ol.client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}

	apiURL := strings.TrimRight(ol.host, "/") + "/api/embeddings"
	results := make([][]float64, len(texts))

	for i, text := range texts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		reqBody, err := json.Marshal(ollamaRequest{
			Model:  ol.model,
			Prompt: text,
		})
		if err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(reqBody))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("ollama api error (status %d): %s", resp.StatusCode, string(body))
		}

		var res ollamaResponse
		err = json.NewDecoder(resp.Body).Decode(&res)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}

		if len(res.Embedding) == 0 {
			return nil, fmt.Errorf("ollama returned empty embeddings")
		}

		results[i] = res.Embedding
	}

	return results, nil
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
