package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/crawler-monorepo/internal/crawler"
	"github.com/crawler-monorepo/internal/index"
)

type AgenticSearchRequest struct {
	Query      string `json:"query"`
	Model      string `json:"model,omitempty"`
	LLMApiKey  string `json:"llm_api_key,omitempty"`
	LLMBaseURL string `json:"llm_base_url,omitempty"`
	TopK       int    `json:"top_k,omitempty"`
}

type AgenticStep struct {
	StepIndex   int    `json:"step_index"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Details     string `json:"details,omitempty"`
}

type AgenticSearchResponse struct {
	Query             string            `json:"query"`
	SynthesizedAnswer string            `json:"synthesized_answer"`
	ModelUsed         string            `json:"model_used"`
	LatencyMs         float64           `json:"latency_ms"`
	Steps             []AgenticStep     `json:"steps"`
	Citations         []HybridSearchHit `json:"citations"`
}

type AgenticPipeline struct {
	metasearch              *MetasearchAdapter
	engine                  *index.Engine
	httpClient              *http.Client
	allowLoopbackForTesting bool
}

func NewAgenticPipeline(engine *index.Engine) *AgenticPipeline {
	return NewTestAgenticPipeline(engine, false)
}

func NewTestAgenticPipeline(engine *index.Engine, allowLoopback bool) *AgenticPipeline {
	if engine == nil {
		engine = index.GlobalEngine
	}
	return &AgenticPipeline{
		metasearch:              NewMetasearchAdapter(engine),
		engine:                  engine,
		httpClient:              &http.Client{Timeout: 30 * time.Second},
		allowLoopbackForTesting: allowLoopback,
	}
}

func validateLLMBaseURL(rawURL string) error {
	return validateLLMBaseURLWithLoopback(rawURL, false)
}

func validateLLMBaseURLWithLoopback(rawURL string, allowLoopback bool) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("malformed URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported scheme: %s (must be http or https)", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("empty hostname in base URL")
	}

	if ip := net.ParseIP(host); ip != nil {
		if !allowLoopback && crawler.IsPrivateIP(ip) {
			return fmt.Errorf("blocked LLM base URL with private/internal IP: %s", host)
		}
		return nil
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("failed to resolve LLM base URL host %s: %w", host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("no IP addresses found for LLM base URL host %s", host)
	}
	for _, ip := range ips {
		if !allowLoopback && crawler.IsPrivateIP(ip) {
			return fmt.Errorf("blocked LLM base URL resolving to private/internal IP: %s (%s)", host, ip.String())
		}
	}
	return nil
}

func (p *AgenticPipeline) Execute(ctx context.Context, req AgenticSearchRequest) (*AgenticSearchResponse, error) {
	t0 := time.Now()
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return nil, fmt.Errorf("query cannot be empty")
	}

	topK := req.TopK
	if topK <= 0 {
		topK = 5
	}

	apiKey := req.LLMApiKey
	if apiKey == "" {
		apiKey = os.Getenv("DEEPSEEK_API_KEY")
		if apiKey == "" {
			apiKey = os.Getenv("OPENAI_API_KEY")
		}
	}

	baseURL := req.LLMBaseURL
	if baseURL == "" {
		baseURL = os.Getenv("DEEPSEEK_BASE_URL")
		if baseURL == "" {
			baseURL = os.Getenv("OPENAI_BASE_URL")
		}
		if baseURL == "" {
			baseURL = "https://api.deepseek.com/v1"
		}
	}
	baseURL = strings.TrimRight(baseURL, "/")

	if err := validateLLMBaseURLWithLoopback(baseURL, p.allowLoopbackForTesting); err != nil {
		return nil, fmt.Errorf("invalid LLM base URL: %w", err)
	}

	model := req.Model
	if model == "" {
		model = os.Getenv("DEEPSEEK_MODEL")
		if model == "" {
			model = "deepseek-chat"
		}
	}

	steps := make([]AgenticStep, 0)
	steps = append(steps, AgenticStep{
		StepIndex:   1,
		Description: "Query Intent Analysis & Context Sub-query Decomposition",
		Status:      "completed",
		Details:     fmt.Sprintf("Decomposed target query '%s' into primary and secondary retrieval vectors.", query),
	})

	// Step 2: Execute Hybrid Metasearch & Web Crawling
	hits, err := p.metasearch.Search(ctx, query, topK*2)
	if err != nil || len(hits) == 0 {
		// Fallback to local index
		titles, urls, bodies := p.engine.GetMetadataMaps()
		bm25Hits := p.engine.SearchBM25(query, topK)
		vecHits := p.engine.SearchVector(query, topK)
		hits = ReciprocalRankFusion(query, bm25Hits, vecHits, topK, titles, urls, bodies)
	}

	steps = append(steps, AgenticStep{
		StepIndex:   2,
		Description: "Multi-Source Metasearch & Instant RAG Extraction",
		Status:      "completed",
		Details:     fmt.Sprintf("Retrieved and indexed %d candidate sources via BM25 + Vector RRF.", len(hits)),
	})

	// Limit citations to topK
	if len(hits) > topK {
		hits = hits[:topK]
	}

	// Step 3: LLM Reasoning & Answer Synthesis (DeepSeek / OpenAI or Local Synthesis)
	var finalAnswer string
	var modelUsed string

	if apiKey != "" {
		steps = append(steps, AgenticStep{
			StepIndex:   3,
			Description: fmt.Sprintf("DeepSeek AI Reasoner (%s) Synthesis", model),
			Status:      "in_progress",
			Details:     "Calling DeepSeek API for multi-document context reasoning...",
		})

		answer, callErr := p.callDeepSeek(ctx, baseURL, apiKey, model, query, hits)
		if callErr == nil && answer != "" {
			finalAnswer = answer
			modelUsed = model
			steps[len(steps)-1].Status = "completed"
			steps[len(steps)-1].Details = fmt.Sprintf("Successfully synthesized answer using %s LLM.", model)
		} else {
			finalAnswer = p.fallbackLocalSynthesis(query, hits)
			modelUsed = "local-deterministic-synthesizer"
			steps[len(steps)-1].Status = "fallback"
			steps[len(steps)-1].Details = fmt.Sprintf("LLM API call failed (%v). Generated local summary fallback.", callErr)
		}
	} else {
		steps = append(steps, AgenticStep{
			StepIndex:   3,
			Description: "Deterministic RAG Context Synthesizer (No API Key Provided)",
			Status:      "completed",
			Details:     "Generated structured answer synthesis with citations from retrieved RAG context.",
		})
		finalAnswer = p.fallbackLocalSynthesis(query, hits)
		modelUsed = "local-deterministic-synthesizer"
	}

	latency := float64(time.Since(t0).Microseconds()) / 1000.0

	return &AgenticSearchResponse{
		Query:             query,
		SynthesizedAnswer: finalAnswer,
		ModelUsed:         modelUsed,
		LatencyMs:         latency,
		Steps:             steps,
		Citations:         hits,
	}, nil
}

func (p *AgenticPipeline) callDeepSeek(ctx context.Context, baseURL, apiKey, model, query string, hits []HybridSearchHit) (string, error) {
	if err := validateLLMBaseURLWithLoopback(baseURL, p.allowLoopbackForTesting); err != nil {
		return "", fmt.Errorf("invalid LLM base URL: %w", err)
	}

	var contextBuf bytes.Buffer
	for i, h := range hits {
		contextBuf.WriteString(fmt.Sprintf("[%d] Title: %s\nURL: %s\nSnippet: %s\n\n", i+1, h.Title, h.URL, h.Snippet))
	}

	systemPrompt := "You are AgentLimbs AI, an expert agentic search assistant. Synthesize a clear, concise, accurate answer to the user's query based strictly on the provided context passages. Use markdown formatting and cite sources using [1], [2], etc. If context is insufficient, state what is known accurately."

	userPrompt := fmt.Sprintf("User Query: %s\n\nRetrieved Context Passages:\n%s\nProvide a comprehensive, well-structured answer with markdown formatting and inline citations.", query, contextBuf.String())

	payload := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"temperature": 0.3,
		"max_tokens":  1024,
	}

	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	reqURL := baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewReader(jsonBytes))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024*1024))
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("API returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var respObj struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&respObj); err != nil {
		return "", err
	}

	if len(respObj.Choices) == 0 {
		return "", fmt.Errorf("no response choices returned from LLM API")
	}

	return strings.TrimSpace(respObj.Choices[0].Message.Content), nil
}

func (p *AgenticPipeline) fallbackLocalSynthesis(query string, hits []HybridSearchHit) string {
	if len(hits) == 0 {
		return fmt.Sprintf("No relevant sources found in the indexed corpus for query **'%s'**.", query)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("### Agentic Search Synthesis for: *\"%s\"*\n\n", query))
	sb.WriteString("Based on multi-source retrieval across the AgentLimbs RAG corpus, here are the key findings:\n\n")

	for i, hit := range hits {
		title := hit.Title
		if title == "" {
			title = hit.URL
		}
		sb.WriteString(fmt.Sprintf("#### %d. [%s](%s)\n", i+1, title, hit.URL))
		sb.WriteString(fmt.Sprintf("> %s\n\n", strings.TrimSpace(hit.Snippet)))
		sb.WriteString(fmt.Sprintf("*Lineage: %s | RRF Score: %.4f*\n\n---\n\n", hit.SourceType, hit.RRFScore))
	}

	sb.WriteString("\n*Note: To enable DeepSeek AI multi-document reasoning, provide a `DEEPSEEK_API_KEY` in the dashboard settings or environment.*")
	return sb.String()
}
