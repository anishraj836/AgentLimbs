package main

import (
	"net/http"
	"os"

	"github.com/crawler-monorepo/agent-service/api"
	"github.com/crawler-monorepo/common/logger"
	"github.com/crawler-monorepo/common/metrics"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

func main() {
	logger.InitLogger(os.Getenv("ENV"))
	defer logger.Sync()

	logger.Log.Info("Starting Agent Service (AI Scrape & Hybrid RAG)...")
	metrics.InitMetricsServer("8089")

	handler := api.NewAgentHandler()

	execMode := os.Getenv("EXECUTION_MODE")
	if execMode == "" {
		execMode = "local"
	}
	apiKey := os.Getenv("AGENT_API_KEY")

	maxReqsPerMin := 1800 // Local / Enterprise On-Premises Mode (30 req/sec)
	if execMode == "cloud" {
		maxReqsPerMin = 300 // Public Cloud SaaS Mode (5 req/sec)
	}

	rateLimiter := api.NewRateLimiter(maxReqsPerMin)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(api.SecurityMiddleware(execMode, apiKey, rateLimiter))

	r.Post("/v1/scrape", handler.Scrape)
	r.Get("/v1/scrape", handler.Scrape)
	r.Post("/v1/extract", handler.Extract)
	r.Post("/v1/agent/query", handler.AgentQuery)
	r.Get("/v1/agent/query", handler.AgentQuery)
	r.Get("/v1/agent/tools", handler.Tools)

	port := os.Getenv("AGENT_PORT")
	if port == "" {
		port = "8090"
	}

	logger.Log.Info("Agent Service listening on port " + port)
	if err := http.ListenAndServe(":"+port, r); err != nil {
		logger.Log.Fatal("Agent Service failed to start", zap.Error(err))
	}
}
