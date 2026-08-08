package api

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/crawler-monorepo/common/utils"
	"github.com/crawler-monorepo/embedding-service/embedder"
	"github.com/crawler-monorepo/internal/crawler"
	"github.com/crawler-monorepo/internal/extractor"
	"github.com/crawler-monorepo/internal/index"
	"github.com/crawler-monorepo/internal/search"
	"github.com/crawler-monorepo/internal/storage"
	"github.com/crawler-monorepo/tokenizer-service/tokenizer"
)

// GetDefaultTTL returns the configured default TTL duration from DEFAULT_TTL_SECONDS env (fallback: 7 days).
func GetDefaultTTL() time.Duration {
	if env := os.Getenv("DEFAULT_TTL_SECONDS"); env != "" {
		if secs, err := strconv.Atoi(env); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return 7 * 24 * time.Hour
}

// ClampTTL validates and clamps LLM-supplied or API-supplied TTL seconds within safe boundaries.
func ClampTTL(llmTTL int) (time.Duration, bool) {
	if llmTTL == 0 {
		return 0, false
	}
	if llmTTL < 0 {
		return GetDefaultTTL(), true
	}

	if llmTTL < 300 {
		return 300 * time.Second, true
	}
	if llmTTL > 30*86400 {
		return 30 * 24 * time.Hour, true
	}

	return time.Duration(llmTTL) * time.Second, true
}

type RateLimiter struct {
	mu        sync.Mutex
	requests  map[string]int
	lastReset time.Time
	maxReqs   int
}

func NewRateLimiter(maxReqsPerMin int) *RateLimiter {
	return &RateLimiter{
		requests:  make(map[string]int),
		lastReset: time.Now(),
		maxReqs:   maxReqsPerMin,
	}
}

func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if time.Since(rl.lastReset) > time.Minute {
		rl.requests = make(map[string]int)
		rl.lastReset = time.Now()
	}

	if rl.requests[ip] >= rl.maxReqs {
		return false
	}
	rl.requests[ip]++
	return true
}

func parseIP(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(s); err == nil {
		s = host
	}
	if parsed := net.ParseIP(s); parsed != nil {
		return parsed.String()
	}
	return ""
}

func isTrustedProxy(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}

	if ip.IsLoopback() {
		return true
	}

	if envProxies := os.Getenv("TRUSTED_PROXIES"); envProxies != "" {
		for _, raw := range strings.Split(envProxies, ",") {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}
			if _, cidr, err := net.ParseCIDR(raw); err == nil {
				if cidr.Contains(ip) {
					return true
				}
			} else if parsed := net.ParseIP(raw); parsed != nil {
				if parsed.Equal(ip) {
					return true
				}
			}
		}
	}

	return false
}

func GetClientIP(r *http.Request) string {
	remoteIP := parseIP(r.RemoteAddr)

	if !isTrustedProxy(remoteIP) {
		if remoteIP != "" {
			return remoteIP
		}
		return r.RemoteAddr
	}

	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		for _, part := range strings.Split(xff, ",") {
			if ip := parseIP(part); ip != "" {
				return ip
			}
		}
	}

	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		if ip := parseIP(xri); ip != "" {
			return ip
		}
	}

	if remoteIP != "" {
		return remoteIP
	}
	return r.RemoteAddr
}

func SecurityMiddleware(mode, apiKey string, limiter *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-API-Key")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}

			ip := GetClientIP(r)

			if !limiter.Allow(ip) {
				http.Error(w, `{"error":"Rate limit exceeded. Please try again later."}`, http.StatusTooManyRequests)
				return
			}

			if apiKey != "" || mode == "cloud" {
				clientKey := r.Header.Get("X-API-Key")
				if clientKey == "" {
					clientKey = r.URL.Query().Get("api_key")
				}
				if apiKey == "" || clientKey != apiKey {
					http.Error(w, `{"error":"Unauthorized: Invalid or missing X-API-Key header"}`, http.StatusUnauthorized)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

type AgentHandler struct {
	httpClient *crawler.Client
}

func NewAgentHandler() *AgentHandler {
	return &AgentHandler{
		httpClient: crawler.NewClient(),
	}
}

type ScrapeRequest struct {
	URL        string `json:"url"`
	TTLSeconds int    `json:"ttl_seconds,omitempty"`
}

type ScrapeResponse struct {
	URL           string  `json:"url"`
	Title         string  `json:"title"`
	Markdown      string  `json:"markdown"`
	TokenEstimate int     `json:"token_estimate"`
	LatencyMs     float64 `json:"latency_ms"`
}

func (h *AgentHandler) Scrape(w http.ResponseWriter, r *http.Request) {
	t0 := time.Now()
	var req ScrapeRequest

	if r.Method == http.MethodPost {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"Invalid JSON body"}`, http.StatusBadRequest)
			return
		}
	} else {
		req.URL = r.URL.Query().Get("url")
		if ttlStr := r.URL.Query().Get("ttl_seconds"); ttlStr != "" {
			req.TTLSeconds, _ = strconv.Atoi(ttlStr)
		}
	}

	if req.URL == "" {
		http.Error(w, `{"error":"URL parameter required"}`, http.StatusBadRequest)
		return
	}

	normURL, err := utils.NormalizeURL(req.URL)
	if err != nil {
		http.Error(w, `{"error":"Invalid URL format"}`, http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	result, err := h.httpClient.Fetch(ctx, normURL)
	if err != nil {
		http.Error(w, `{"error":"Failed to fetch URL: `+err.Error()+`"}`, http.StatusBadGateway)
		return
	}
	defer result.Response.Body.Close()

	limitedBody := io.LimitReader(result.Response.Body, 10*1024*1024)
	htmlBytes, err := io.ReadAll(limitedBody)
	if err != nil {
		http.Error(w, `{"error":"Failed to read response body"}`, http.StatusInternalServerError)
		return
	}

	mdText, tokens, title := extractor.ConvertHTMLToMarkdown(result.FinalURL, htmlBytes, "clean_rag")

	cleanDoc, _ := extractor.ProcessRawHTML(result.FinalURL, htmlBytes)
	tokenizedDoc := tokenizer.TokenizePipeline(cleanDoc.URL, cleanDoc.Title, cleanDoc.Body)
	index.GlobalEngine.IndexDocument(
		tokenizedDoc.URL,
		tokenizedDoc.Title,
		tokenizedDoc.CleanBody,
		tokenizedDoc.TermPositions,
		tokenizedDoc.TotalTokens,
	)

	if ttlDuration, hasTTL := ClampTTL(req.TTLSeconds); hasTTL {
		_ = storage.SaveCrawledDocumentWithTTL(
			r.Context(),
			tokenizedDoc.URL,
			tokenizedDoc.Title,
			tokenizedDoc.CleanBody,
			tokenizedDoc.TotalTokens,
			"web_crawled",
			tokenizedDoc.URL,
			ttlDuration,
		)
	}
	embedder.IndexDocumentVector(cleanDoc.URL, cleanDoc.Title, cleanDoc.Body)

	latency := float64(time.Since(t0).Microseconds()) / 1000.0

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ScrapeResponse{
		URL:           result.FinalURL,
		Title:         title,
		Markdown:      mdText,
		TokenEstimate: tokens,
		LatencyMs:     latency,
	})
}

type AgentQueryRequest struct {
	Query string `json:"query"`
	TopK  int    `json:"top_k"`
}

func (h *AgentHandler) AgentQuery(w http.ResponseWriter, r *http.Request) {
	t0 := time.Now()
	var req AgentQueryRequest

	if r.Method == http.MethodPost {
		json.NewDecoder(r.Body).Decode(&req)
	} else {
		req.Query = r.URL.Query().Get("q")
		req.TopK, _ = strconv.Atoi(r.URL.Query().Get("top_k"))
	}

	if req.TopK <= 0 {
		req.TopK = 5
	}

	titles, urls, bodies := index.GlobalEngine.GetMetadataMaps()
	bm25Hits := index.RankDocuments(
		req.Query,
		index.GlobalEngine.Inverted,
		titles,
		urls,
		bodies,
		req.TopK*2,
	)

	queryVec := index.GenerateFeatureVector(req.Query, 128)
	vecHits := embedder.GlobalVectorIndex.SearchNearest(queryVec, req.TopK*2)

	fusedHits := search.ReciprocalRankFusion(bm25Hits, vecHits, req.TopK)

	latency := float64(time.Since(t0).Microseconds()) / 1000.0

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"query":      req.Query,
		"latency_ms": latency,
		"total_hits": len(fusedHits),
		"results":    fusedHits,
	})
}

func (h *AgentHandler) Tools(w http.ResponseWriter, r *http.Request) {
	tools := []map[string]interface{}{
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "agent_limbs_scrape",
				"description": "Fetch and convert any website URL into clean, token-efficient Github-Flavored Markdown for LLMs.",
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"url": map[string]interface{}{
							"type":        "string",
							"description": "The target website URL to scrape (e.g. https://golang.org)",
						},
					},
					"required": []string{"url"},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "agent_limbs_hybrid_search",
				"description": "Search the indexed web corpus using Hybrid RRF (BM25 Keyword + AI Vector Semantic Similarity).",
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"query": map[string]interface{}{
							"type":        "string",
							"description": "The natural language search query or topic",
						},
						"top_k": map[string]interface{}{
							"type":        "integer",
							"description": "Number of top ranked results to return (default 5)",
						},
					},
					"required": []string{"query"},
				},
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"provider": "AgentLimbs AI Agent Tools",
		"version":  "1.0.0",
		"tools":    tools,
	})
}

type ExtractRequest struct {
	URL    string   `json:"url"`
	Fields []string `json:"fields"`
}

func (h *AgentHandler) Extract(w http.ResponseWriter, r *http.Request) {
	var req ExtractRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid JSON body"}`, http.StatusBadRequest)
		return
	}

	normURL, err := utils.NormalizeURL(req.URL)
	if err != nil {
		http.Error(w, `{"error":"Invalid URL format"}`, http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	result, err := h.httpClient.Fetch(ctx, normURL)
	if err != nil {
		http.Error(w, `{"error":"Failed to fetch URL: `+err.Error()+`"}`, http.StatusBadGateway)
		return
	}
	defer result.Response.Body.Close()

	limitedBody := io.LimitReader(result.Response.Body, 10*1024*1024)
	htmlBytes, _ := io.ReadAll(limitedBody)

	mdText, _, title := extractor.ConvertHTMLToMarkdown(result.FinalURL, htmlBytes, "clean_rag")
	extractedData := extractor.ExtractFields(mdText, req.Fields)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"url":       result.FinalURL,
		"title":     title,
		"extracted": extractedData,
	})
}
