package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/crawler-monorepo/agent-service/api"
	"github.com/crawler-monorepo/common/logger"
	"github.com/crawler-monorepo/common/utils"
	"github.com/crawler-monorepo/embedding-service/embedder"
	"github.com/crawler-monorepo/internal/crawler"
	"github.com/crawler-monorepo/internal/extractor"
	"github.com/crawler-monorepo/internal/index"
	"github.com/crawler-monorepo/internal/search"
	"github.com/crawler-monorepo/internal/storage"
	"github.com/crawler-monorepo/tokenizer-service/tokenizer"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

type EmbeddedServer struct {
	httpClient *crawler.Client
	dataDir    string
}

func NewEmbeddedServer(dataDir string) *EmbeddedServer {
	if dataDir == "" {
		dataDir = "data"
	}
	return &EmbeddedServer{
		httpClient: crawler.NewClient(),
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
	tokenizedDoc := tokenizer.TokenizePipeline(cleanDoc.URL, cleanDoc.Title, cleanDoc.Body)
	index.GlobalEngine.IndexDocumentWithSource(
		tokenizedDoc.URL,
		tokenizedDoc.Title,
		tokenizedDoc.CleanBody,
		tokenizedDoc.TermPositions,
		tokenizedDoc.TotalTokens,
		"embedded_scraped",
		res.FinalURL,
	)

	embedder.IndexDocumentVector(cleanDoc.URL, cleanDoc.Title, cleanDoc.Body)

	if ttlDuration, hasTTL := api.ClampTTL(req.TTLSeconds); hasTTL {
		_ = storage.SaveCrawledDocumentWithTTL(
			r.Context(),
			tokenizedDoc.URL,
			tokenizedDoc.Title,
			tokenizedDoc.CleanBody,
			tokenizedDoc.TotalTokens,
			"embedded_scraped",
			res.FinalURL,
			ttlDuration,
		)
	} else {
		_ = storage.SaveCrawledDocument(
			r.Context(),
			tokenizedDoc.URL,
			tokenizedDoc.Title,
			tokenizedDoc.CleanBody,
			tokenizedDoc.TotalTokens,
			"embedded_scraped",
			res.FinalURL,
		)
	}

	saveStorage(s.dataDir)

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
				apiKey = r.URL.Query().Get("api_key")
			}
			if apiKey != expectedKey {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"Unauthorized"}`))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *EmbeddedServer) SetupRouter() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/health", s.HealthHandler)

	r.Group(func(r chi.Router) {
		r.Use(SecurityMiddleware)
		r.Post("/v1/scrape", s.ScrapeHandler)
		r.Get("/v1/scrape", s.ScrapeHandler)
		r.Post("/v1/search", s.SearchHandler)
		r.Get("/v1/search", s.SearchHandler)
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
		if err := embedder.GlobalVectorIndex.LoadSnapshot(vectorPath); err == nil {
			logger.Log.Info("Loaded vector index snapshot from file fallback: " + vectorPath)
		}
	}

	docs, err := storage.GetCrawledDocuments(context.Background())
	if err == nil && len(docs) > 0 {
		for _, d := range docs {
			tokDoc := tokenizer.TokenizePipeline(d.URL, d.Title, d.CleanBody)
			index.GlobalEngine.IndexDocumentWithSource(
				tokDoc.URL,
				tokDoc.Title,
				tokDoc.CleanBody,
				tokDoc.TermPositions,
				tokDoc.TotalTokens,
				d.SourceType,
				d.SourceURL,
			)
			embedder.IndexDocumentVector(d.URL, d.Title, d.CleanBody)
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
	_ = embedder.GlobalVectorIndex.SaveSnapshot(vectorPath)
}

func main() {
	logger.InitLogger(os.Getenv("ENV"))
	defer logger.Sync()

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
		Addr:    ":" + port,
		Handler: router,
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
