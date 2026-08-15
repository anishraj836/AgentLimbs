package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/crawler-monorepo/agent-service/api"
	"github.com/crawler-monorepo/common/config"
	"github.com/crawler-monorepo/common/logger"
	appMiddleware "github.com/crawler-monorepo/common/middleware"
	"github.com/crawler-monorepo/common/redis"
	"github.com/crawler-monorepo/common/tracing"
	"github.com/crawler-monorepo/internal/auth"
	"github.com/crawler-monorepo/internal/index"
	"github.com/crawler-monorepo/internal/storage"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

func main() {
	logger.InitLogger(os.Getenv("ENV"))
	defer logger.Sync()

	if _, err := config.LoadAndValidate(); err != nil {
		logger.Log.Fatal("Configuration validation error", zap.Error(err))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	storage.InitDB(os.Getenv("DATABASE_URL"))
	defer storage.CloseDB()
	defer redis.CloseRedis()
	if err := index.GlobalEngine.LoadFromDB(context.Background()); err != nil {
		logger.Log.Info("No existing persisted corpus loaded from DB into agent-service memory")
	}

	handler := api.NewAgentHandler()

	execMode := os.Getenv("EXECUTION_MODE")
	if execMode == "" {
		execMode = "local"
	}
	apiKey := os.Getenv("AGENT_API_KEY")
	// Note: Any exposed API keys must be rotated.
	logger.Log.Info("SECURITY NOTE: Any exposed API keys must be rotated.")

	maxReqsPerMin := 1800 // Local / Enterprise On-Premises Mode (30 req/sec)
	if execMode == "cloud" {
		maxReqsPerMin = 300 // Public Cloud SaaS Mode (5 req/sec)
	}

	rateLimiter := api.NewRateLimiter(maxReqsPerMin)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(appMiddleware.RequestIDMiddleware)
	r.Use(tracing.TracingMiddleware)
	r.Use(auth.TenantMiddleware)
	r.Use(api.SecurityMiddleware(execMode, apiKey, rateLimiter))

	r.Get("/health", handler.Health)
	r.Get("/healthz", handler.Healthz)
	r.Get("/livez", handler.Livez)
	r.Get("/readyz", handler.Readyz)
	r.Get("/v1/health", handler.Health)
	r.Get("/v1/healthz", handler.Healthz)
	r.Get("/v1/livez", handler.Livez)
	r.Get("/v1/readyz", handler.Readyz)
	r.Post("/v1/scrape", handler.Scrape)
	r.Get("/v1/scrape", handler.Scrape)
	r.Post("/v1/extract", handler.Extract)
	r.Post("/v1/agent/query", handler.AgentQuery)
	r.Get("/v1/agent/query", handler.AgentQuery)
	r.Post("/v1/web-search", handler.WebSearch)
	r.Get("/v1/web-search", handler.WebSearch)
	r.Post("/v1/agentic-search", handler.AgenticSearch)
	r.Get("/v1/agentic-search", handler.AgenticSearch)
	r.Get("/v1/agent/tools", handler.Tools)

	port := os.Getenv("AGENT_PORT")
	if port == "" {
		port = "8090"
	}

	logger.Log.Info("Agent Service listening on port " + port)

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           r,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Log.Fatal("Agent Service failed to start", zap.Error(err))
		}
	}()

	<-ctx.Done()
	logger.Log.Info("Shutting down Agent Service gracefully...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Log.Error("Agent Service forced to shutdown", zap.Error(err))
	}
}
