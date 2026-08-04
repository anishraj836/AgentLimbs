package main

import (
	"net/http"
	"os"

	"github.com/crawler-monorepo/common/logger"
	"github.com/crawler-monorepo/common/metrics"
	"github.com/crawler-monorepo/indexer-service/indexer"
	"github.com/crawler-monorepo/search-service/api"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

func main() {
	logger.InitLogger(os.Getenv("ENV"))
	defer logger.Sync()

	logger.Log.Info("Starting Search Service...")
	metrics.InitMetricsServer("8087")

	handler := api.NewSearchHandler(indexer.GlobalEngine)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Post("/search", handler.Search)
	r.Get("/search", handler.Search)
	r.Get("/autocomplete", handler.Autocomplete)
	r.Get("/document/{id}", handler.GetDocument)
	r.Get("/stats", handler.Stats)
	r.Get("/health", handler.Health)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8088"
	}

	logger.Log.Info("Search Service listening on port " + port)
	if err := http.ListenAndServe(":"+port, r); err != nil {
		logger.Log.Fatal("Search Service failed", zap.Error(err))
	}
}
