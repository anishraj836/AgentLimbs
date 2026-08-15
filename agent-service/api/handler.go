package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/crawler-monorepo/common/kafka"
	"github.com/crawler-monorepo/common/stemmer"
	"github.com/crawler-monorepo/common/stopwords"
	"github.com/crawler-monorepo/common/utils"
	"github.com/crawler-monorepo/internal/crawler"
	"github.com/crawler-monorepo/internal/extractor"
	"github.com/crawler-monorepo/internal/index"
	"github.com/crawler-monorepo/internal/search"
	"github.com/crawler-monorepo/internal/storage"
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

func getSubnet(ipStr string) string {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return ipStr
	}
	if ip4 := ip.To4(); ip4 != nil {
		return fmt.Sprintf("%d.%d.%d.0/24", ip4[0], ip4[1], ip4[2])
	}
	if len(ip) == net.IPv6len {
		return fmt.Sprintf("%x:%x:%x:%x::/64",
			uint16(ip[0])<<8|uint16(ip[1]),
			uint16(ip[2])<<8|uint16(ip[3]),
			uint16(ip[4])<<8|uint16(ip[5]),
			uint16(ip[6])<<8|uint16(ip[7]))
	}
	return ipStr
}

type RateLimiter struct {
	mu           sync.Mutex
	requests     map[string]int
	subnetReqs   map[string]int
	lastReset    time.Time
	maxReqs      int
	maxSubnetReq int
}

func NewRateLimiter(maxReqsPerMin int) *RateLimiter {
	return &RateLimiter{
		requests:     make(map[string]int),
		subnetReqs:   make(map[string]int),
		lastReset:    time.Now(),
		maxReqs:      maxReqsPerMin,
		maxSubnetReq: maxReqsPerMin * 5,
	}
}

func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if time.Since(rl.lastReset) > time.Minute {
		rl.requests = make(map[string]int)
		rl.subnetReqs = make(map[string]int)
		rl.lastReset = time.Now()
	}

	subnet := getSubnet(ip)

	if len(rl.requests) >= 10000 {
		if _, exists := rl.requests[ip]; !exists {
			return false
		}
	}

	if rl.requests[ip] >= rl.maxReqs {
		return false
	}
	if rl.subnetReqs[subnet] >= rl.maxSubnetReq {
		return false
	}

	rl.requests[ip]++
	rl.subnetReqs[subnet]++
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
	if remoteIP == "" {
		remoteIP = r.RemoteAddr
	}

	// Untrusted host early exit
	if !isTrustedProxy(remoteIP) {
		return remoteIP
	}

	// Right-to-left XFF parsing for trusted proxies
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		for i := len(parts) - 1; i >= 0; i-- {
			ip := parseIP(parts[i])
			if ip != "" && !isTrustedProxy(ip) {
				return ip
			}
		}
		for _, part := range parts {
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

	return remoteIP
}

func SecurityMiddleware(mode, apiKey string, limiter *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-API-Key, Authorization, X-Tenant-ID, traceparent, X-Request-ID")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}

			path := r.URL.Path
			if path == "/health" || path == "/healthz" || path == "/livez" || path == "/readyz" ||
				path == "/v1/health" || path == "/v1/healthz" || path == "/v1/livez" || path == "/v1/readyz" {
				next.ServeHTTP(w, r)
				return
			}

			if r.Method == http.MethodPost || r.Method == http.MethodPut {
				r.Body = http.MaxBytesReader(w, r.Body, 2*1024*1024)
			}

			ip := GetClientIP(r)

			if !limiter.Allow(ip) {
				http.Error(w, `{"error":"Rate limit exceeded. Please try again later."}`, http.StatusTooManyRequests)
				return
			}

			if mode == "cloud" && apiKey == "" {
				http.Error(w, `{"error":"Unauthorized: Cloud mode requires non-empty server API key"}`, http.StatusUnauthorized)
				return
			}

			if apiKey != "" {
				clientKey := r.Header.Get("X-API-Key")
				if clientKey == "" {
					authHeader := r.Header.Get("Authorization")
					if strings.HasPrefix(authHeader, "Bearer ") {
						clientKey = strings.TrimPrefix(authHeader, "Bearer ")
					}
				}
				if clientKey == "" {
					clientKey = r.URL.Query().Get("api_key")
				}
				if clientKey == "" {
					http.Error(w, `{"error":"Unauthorized: Invalid or missing X-API-Key header or Authorization Bearer token"}`, http.StatusUnauthorized)
					return
				}
				h1 := sha256.Sum256([]byte(clientKey))
				h2 := sha256.Sum256([]byte(apiKey))
				if subtle.ConstantTimeCompare(h1[:], h2[:]) != 1 {
					http.Error(w, `{"error":"Unauthorized: Invalid or missing X-API-Key header or Authorization Bearer token"}`, http.StatusUnauthorized)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

type AgentHandler struct {
	httpClient *crawler.Client
	scrapeSem  chan struct{}
}

func NewAgentHandler() *AgentHandler {
	return &AgentHandler{
		httpClient: crawler.NewClient(),
		scrapeSem:  make(chan struct{}, 20),
	}
}

type ScrapeRequest struct {
	URL        string `json:"url"`
	Mode       string `json:"mode,omitempty"`
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
	if h.scrapeSem != nil {
		select {
		case h.scrapeSem <- struct{}{}:
			defer func() { <-h.scrapeSem }()
		default:
			http.Error(w, `{"error":"Scrape worker pool capacity reached (20 active workers)"}`, http.StatusServiceUnavailable)
			return
		}
	}

	t0 := time.Now()
	var req ScrapeRequest

	if r.Method == http.MethodPost {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"Invalid JSON body"}`, http.StatusBadRequest)
			return
		}
	} else {
		req.URL = r.URL.Query().Get("url")
		req.Mode = r.URL.Query().Get("mode")
		if ttlStr := r.URL.Query().Get("ttl_seconds"); ttlStr != "" {
			req.TTLSeconds, _ = strconv.Atoi(ttlStr)
		}
	}

	if req.Mode == "" {
		req.Mode = "clean_rag"
	}
	if req.Mode != "clean_rag" && req.Mode != "preserve_links" && req.Mode != "raw" {
		http.Error(w, `{"error":"Invalid mode. Allowed values: clean_rag, preserve_links, raw"}`, http.StatusBadRequest)
		return
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

	contentType := result.Response.Header.Get("Content-Type")
	mdText, tokens, title, extractErr := extractor.ExtractDocumentText(result.FinalURL, contentType, htmlBytes, req.Mode)
	if extractErr != nil {
		http.Error(w, `{"error":"`+extractErr.Error()+`"}`, http.StatusBadRequest)
		return
	}

	cleanDoc, _ := extractor.ProcessRawHTML(result.FinalURL, htmlBytes)
	if cleanDoc != nil {
		rawTokens := strings.Fields(strings.ToLower(cleanDoc.Body))
		termPositions := make(map[string][]int)
		for idx, raw := range rawTokens {
			clean := strings.Trim(raw, ".,!?:;\"'()[]{}")
			if clean == "" || stopwords.IsStopword(clean) {
				continue
			}
			stemmed := stemmer.Stem(clean)
			termPositions[stemmed] = append(termPositions[stemmed], idx)
		}

		index.GlobalEngine.IndexDocumentWithSource(cleanDoc.URL, cleanDoc.Title, cleanDoc.Body, termPositions, tokens, "web_crawled", cleanDoc.URL)

		if ttlDuration, hasTTL := ClampTTL(req.TTLSeconds); hasTTL {
			_ = storage.SaveCrawledDocumentWithTTL(
				r.Context(),
				cleanDoc.URL,
				cleanDoc.Title,
				cleanDoc.Body,
				tokens,
				"web_crawled",
				cleanDoc.URL,
				ttlDuration,
			)
		}
	}

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
	if req.TopK > 100 {
		req.TopK = 100
	}

	titles, urls, bodies := index.GlobalEngine.GetMetadataMaps()
	bm25Hits := index.GlobalEngine.SearchBM25(req.Query, req.TopK*2)

	vecHits := index.GlobalEngine.SearchVector(req.Query, req.TopK*2)

	fusedHits := search.ReciprocalRankFusion(req.Query, bm25Hits, vecHits, req.TopK, titles, urls, bodies)

	latency := float64(time.Since(t0).Microseconds()) / 1000.0

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"query":      req.Query,
		"latency_ms": latency,
		"total_hits": len(fusedHits),
		"results":    fusedHits,
	})
}

func (h *AgentHandler) WebSearch(w http.ResponseWriter, r *http.Request) {
	t0 := time.Now()
	var req AgentQueryRequest

	if r.Method == http.MethodPost {
		_ = json.NewDecoder(r.Body).Decode(&req)
	} else {
		req.Query = r.URL.Query().Get("q")
		if req.Query == "" {
			req.Query = r.URL.Query().Get("query")
		}
		req.TopK, _ = strconv.Atoi(r.URL.Query().Get("top_k"))
	}

	if req.TopK <= 0 {
		req.TopK = 5
	}
	if req.TopK > 50 {
		req.TopK = 50
	}

	adapter := search.NewMetasearchAdapter(index.GlobalEngine)
	hits, err := adapter.Search(r.Context(), req.Query, req.TopK)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	latency := float64(time.Since(t0).Microseconds()) / 1000.0

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"query":      req.Query,
		"latency_ms": latency,
		"total_hits": len(hits),
		"results":    hits,
	})
}

func (h *AgentHandler) AgenticSearch(w http.ResponseWriter, r *http.Request) {
	var req search.AgenticSearchRequest

	if r.Method == http.MethodPost {
		_ = json.NewDecoder(r.Body).Decode(&req)
	} else {
		req.Query = r.URL.Query().Get("q")
		if req.Query == "" {
			req.Query = r.URL.Query().Get("query")
		}
		req.Model = r.URL.Query().Get("model")
		req.LLMApiKey = r.URL.Query().Get("llm_api_key")
		req.LLMBaseURL = r.URL.Query().Get("llm_base_url")
		req.TopK, _ = strconv.Atoi(r.URL.Query().Get("top_k"))
	}

	pipeline := search.NewAgenticPipeline(index.GlobalEngine)
	resp, err := pipeline.Execute(r.Context(), req)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
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
		"provider": "WebLimbAI Agent Tools",
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

func (h *AgentHandler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok","service":"agent"}`))
}

func (h *AgentHandler) Healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok","service":"agent"}`))
}

func (h *AgentHandler) Livez(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"alive","service":"agent"}`))
}

func (h *AgentHandler) Readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	checks := make(map[string]string)
	allHealthy := true

	if err := storage.PingDB(ctx); err != nil {
		checks["database"] = fmt.Sprintf("error: %v", err)
		allHealthy = false
	} else {
		checks["database"] = "ok"
	}

	if err := kafka.CheckKafkaReadiness(ctx); err != nil {
		checks["kafka"] = fmt.Sprintf("error: %v", err)
		allHealthy = false
	} else {
		checks["kafka"] = "ok"
	}

	w.Header().Set("Content-Type", "application/json")
	if !allHealthy {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "unhealthy",
			"service": "agent",
			"checks":  checks,
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ready",
		"service": "agent",
		"checks":  checks,
	})
}
