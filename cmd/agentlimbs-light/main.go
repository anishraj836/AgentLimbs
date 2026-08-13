package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/crawler-monorepo/agent-service/api"
	"github.com/crawler-monorepo/common/config"
	"github.com/crawler-monorepo/common/logger"
	appMiddleware "github.com/crawler-monorepo/common/middleware"
	"github.com/crawler-monorepo/common/ratelimit"
	"github.com/crawler-monorepo/common/stemmer"
	"github.com/crawler-monorepo/common/stopwords"
	"github.com/crawler-monorepo/common/utils"
	"github.com/crawler-monorepo/internal/crawler"
	"github.com/crawler-monorepo/internal/extractor"
	"github.com/crawler-monorepo/internal/index"
	"github.com/crawler-monorepo/internal/search"
	"github.com/crawler-monorepo/internal/storage"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

var (
	saveTriggerChan = make(chan string, 100)
	saveTriggerOnce sync.Once
)

func triggerDebouncedSave(dataDir string) {
	saveTriggerOnce.Do(func() {
		go func() {
			var timer *time.Timer
			var latestDir string
			for {
				select {
				case dir, ok := <-saveTriggerChan:
					if !ok {
						return
					}
					latestDir = dir
					if timer == nil {
						timer = time.NewTimer(300 * time.Millisecond)
					} else {
						timer.Reset(300 * time.Millisecond)
					}
				case <-func() <-chan time.Time {
					if timer == nil {
						return nil
					}
					return timer.C
				}():
					if latestDir != "" {
						saveStorage(latestDir)
						latestDir = ""
					}
					timer = nil
				}
			}
		}()
	})

	select {
	case saveTriggerChan <- dataDir:
	default:
	}
}

type EmbeddedServer struct {
	httpClient *crawler.Client
	dataDir    string
}

func NewEmbeddedServer(dataDir string) *EmbeddedServer {
	return NewEmbeddedServerWithClient(dataDir, crawler.NewClient())
}

func NewEmbeddedServerWithClient(dataDir string, httpClient *crawler.Client) *EmbeddedServer {
	if dataDir == "" {
		dataDir = "data"
	}
	if httpClient == nil {
		httpClient = crawler.NewClient()
	}
	return &EmbeddedServer{
		httpClient: httpClient,
		dataDir:    dataDir,
	}
}

func (s *EmbeddedServer) HTTPClient() *crawler.Client {
	return s.httpClient
}

func (s *EmbeddedServer) HealthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok","mode":"single_binary_embedded"}`))
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

func (s *EmbeddedServer) ScrapeHandler(w http.ResponseWriter, r *http.Request) {
	t0 := time.Now()
	var req ScrapeRequest

	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 1*1024*1024)
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

	if req.URL == "" {
		http.Error(w, `{"error":"URL parameter required"}`, http.StatusBadRequest)
		return
	}

	if req.Mode == "" {
		req.Mode = "clean_rag"
	}
	if req.Mode != "clean_rag" && req.Mode != "preserve_links" && req.Mode != "raw" {
		http.Error(w, `{"error":"Invalid mode parameter"}`, http.StatusBadRequest)
		return
	}

	normURL, err := utils.NormalizeURL(req.URL)
	if err != nil {
		http.Error(w, `{"error":"Invalid URL format"}`, http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	fetchURL := utils.TransformGitHubURL(normURL)
	res, err := s.httpClient.Fetch(ctx, fetchURL)
	if err != nil {
		http.Error(w, `{"error":"Failed to fetch URL: `+err.Error()+`"}`, http.StatusBadGateway)
		return
	}
	defer res.Response.Body.Close()

	limitedBody := io.LimitReader(res.Response.Body, 10*1024*1024)
	htmlBytes, err := io.ReadAll(limitedBody)
	if err != nil {
		http.Error(w, `{"error":"Failed to read response body"}`, http.StatusInternalServerError)
		return
	}

	mdText, tokens, title := extractor.ConvertHTMLToMarkdown(res.FinalURL, htmlBytes, req.Mode)

	cleanDoc, _ := extractor.ProcessRawHTML(res.FinalURL, htmlBytes)
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

		index.GlobalEngine.IndexDocumentWithSource(cleanDoc.URL, cleanDoc.Title, cleanDoc.Body, termPositions, tokens, "embedded_scraped", res.FinalURL)

		if ttlDuration, hasTTL := api.ClampTTL(req.TTLSeconds); hasTTL {
			_ = storage.SaveCrawledDocumentWithTTL(
				r.Context(),
				cleanDoc.URL,
				cleanDoc.Title,
				cleanDoc.Body,
				tokens,
				"embedded_scraped",
				res.FinalURL,
				ttlDuration,
			)
		}
	}

	triggerDebouncedSave(s.dataDir)

	latency := float64(time.Since(t0).Microseconds()) / 1000.0

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(ScrapeResponse{
		URL:           res.FinalURL,
		Title:         title,
		Markdown:      mdText,
		TokenEstimate: tokens,
		LatencyMs:     latency,
	})
}

type SearchRequest struct {
	Query string `json:"query"`
	TopK  int    `json:"top_k"`
}

type SearchResponse struct {
	Query     string                  `json:"query"`
	LatencyMs float64                 `json:"latency_ms"`
	TotalHits int                     `json:"total_hits"`
	Results   []search.HybridSearchHit `json:"results"`
}

func (s *EmbeddedServer) SearchHandler(w http.ResponseWriter, r *http.Request) {
	t0 := time.Now()
	var req SearchRequest

	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 1*1024*1024)
		json.NewDecoder(r.Body).Decode(&req)
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
	if req.TopK > 100 {
		req.TopK = 100
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

	vecHits := index.GlobalEngine.SearchVector(req.Query, req.TopK*2)

	fusedHits := search.ReciprocalRankFusion(req.Query, bm25Hits, vecHits, req.TopK, titles, urls, bodies)

	latency := float64(time.Since(t0).Microseconds()) / 1000.0

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(SearchResponse{
		Query:     req.Query,
		LatencyMs: latency,
		TotalHits: len(fusedHits),
		Results:   fusedHits,
	})
}

func SecurityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expectedKey := os.Getenv("AGENT_API_KEY")
		if expectedKey != "" {
			apiKey := r.Header.Get("X-API-Key")
			if apiKey == "" {
				authHeader := r.Header.Get("Authorization")
				if strings.HasPrefix(authHeader, "Bearer ") {
					apiKey = strings.TrimPrefix(authHeader, "Bearer ")
				}
			}
			if apiKey == "" {
				apiKey = r.URL.Query().Get("api_key")
			}
			if apiKey == "" || subtle.ConstantTimeCompare([]byte(apiKey), []byte(expectedKey)) != 1 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"Unauthorized"}`))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

type AutocompleteResponse struct {
	Prefix      string                     `json:"prefix"`
	Suggestions []index.AutocompleteResult `json:"suggestions"`
}

func (s *EmbeddedServer) AutocompleteHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	limitStr := r.URL.Query().Get("limit")
	limit := 5
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
	}
	if limit > 50 {
		limit = 50
	}

	results := index.GlobalEngine.Trie.SearchPrefix(q, limit)
	if results == nil {
		results = make([]index.AutocompleteResult, 0)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(AutocompleteResponse{
		Prefix:      q,
		Suggestions: results,
	})
}

func (s *EmbeddedServer) WebSearchHandler(w http.ResponseWriter, r *http.Request) {
	t0 := time.Now()
	var req SearchRequest

	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 1*1024*1024)
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
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(SearchResponse{
		Query:     req.Query,
		LatencyMs: latency,
		TotalHits: len(hits),
		Results:   hits,
	})
}

func (s *EmbeddedServer) AgenticSearchHandler(w http.ResponseWriter, r *http.Request) {
	var req search.AgenticSearchRequest

	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 1*1024*1024)
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

func (s *EmbeddedServer) SetupRouter() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(appMiddleware.RequestIDMiddleware)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-API-Key, Authorization")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}
			next.ServeHTTP(w, r)
		})
	})

	r.Get("/health", s.HealthHandler)

	r.Group(func(r chi.Router) {
		r.Use(SecurityMiddleware)
		r.Use(ratelimit.RateLimiterMiddleware(50.0, 100.0))
		r.Post("/v1/scrape", s.ScrapeHandler)
		r.Get("/v1/scrape", s.ScrapeHandler)
		r.Post("/v1/search", s.SearchHandler)
		r.Get("/v1/search", s.SearchHandler)
		r.Post("/v1/web-search", s.WebSearchHandler)
		r.Get("/v1/web-search", s.WebSearchHandler)
		r.Post("/v1/agentic-search", s.AgenticSearchHandler)
		r.Get("/v1/agentic-search", s.AgenticSearchHandler)
		r.Get("/v1/autocomplete", s.AutocompleteHandler)
	})

	return r
}

// GetJanitorInterval returns the configured janitor interval duration from JANITOR_INTERVAL env (fallback: 15 minutes).
func GetJanitorInterval() time.Duration {
	if env := os.Getenv("JANITOR_INTERVAL"); env != "" {
		if d, err := time.ParseDuration(env); err == nil && d > 0 {
			return d
		}
	}
	return 15 * time.Minute
}

func initStorage(dataDir string) {
	if dataDir == "" {
		dataDir = "data"
	}
	_ = os.MkdirAll(dataDir, 0755)

	indexPath := filepath.Join(dataDir, "inverted_index.json")
	if _, err := os.Stat(indexPath); err == nil {
		if err := index.GlobalEngine.Inverted.LoadSnapshot(indexPath); err == nil {
			logger.Log.Info("Loaded inverted index snapshot from file fallback: " + indexPath)
		}
	}

	vectorPath := filepath.Join(dataDir, "vector_index.json")
	if _, err := os.Stat(vectorPath); err == nil {
		if err := index.GlobalEngine.Vector.LoadSnapshot(vectorPath); err == nil {
			logger.Log.Info("Loaded vector index snapshot from file fallback: " + vectorPath)
		}
	}

	docs, err := storage.GetCrawledDocuments(context.Background())
	if err == nil && len(docs) > 0 {
		for _, d := range docs {
			rawTokens := strings.Fields(strings.ToLower(d.CleanBody))
			termPositions := make(map[string][]int)
			for idx, raw := range rawTokens {
				clean := strings.Trim(raw, ".,!?:;\"'()[]{}")
				if clean == "" || stopwords.IsStopword(clean) {
					continue
				}
				stemmed := stemmer.Stem(clean)
				termPositions[stemmed] = append(termPositions[stemmed], idx)
			}
			index.GlobalEngine.IndexDocumentWithSource(d.URL, d.Title, d.CleanBody, termPositions, d.TotalTokens, d.SourceType, d.SourceURL)
		}
		logger.Log.Info(fmt.Sprintf("Hydrated %d documents into memory from file fallback", len(docs)))
	}

	// Start background TTL Janitor routine to purge expired pages on JANITOR_INTERVAL (fallback: 15m)
	index.GlobalEngine.StartTTLJanitor(context.Background(), GetJanitorInterval())
}

func saveStorage(dataDir string) {
	if dataDir == "" {
		dataDir = "data"
	}
	_ = os.MkdirAll(dataDir, 0755)

	indexPath := filepath.Join(dataDir, "inverted_index.json")
	_ = index.GlobalEngine.Inverted.SaveSnapshot(indexPath)

	vectorPath := filepath.Join(dataDir, "vector_index.json")
	_ = index.GlobalEngine.Vector.SaveSnapshot(vectorPath)
}

func main() {
	logger.InitLogger(os.Getenv("ENV"))
	defer logger.Sync()

	if _, err := config.LoadAndValidate(); err != nil {
		logger.Log.Fatal("Configuration validation error", zap.Error(err))
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "data"
	}

	initStorage(dataDir)

	server := NewEmbeddedServer(dataDir)
	router := server.SetupRouter()

	httpServer := &http.Server{
		Addr:              ":" + port,
		Handler:           router,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
	}

	logger.Log.Info("AgentLimbs Light Single-Binary Server starting on port " + port)

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Log.Fatal("Server error", zap.Error(err))
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	logger.Log.Info("Shutting down server gracefully...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		logger.Log.Error("Server forced to shutdown", zap.Error(err))
	}
}
