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

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Post("/v1/scrape", handler.Scrape)
	r.Get("/v1/scrape", handler.Scrape)
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
