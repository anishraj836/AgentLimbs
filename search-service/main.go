package main

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/crawler-monorepo/common/db"
	"github.com/crawler-monorepo/common/logger"
	"github.com/crawler-monorepo/indexer-service/indexer"
	"github.com/crawler-monorepo/search-service/api"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

func main() {
	logger.InitLogger(os.Getenv("ENV"))
	defer logger.Sync()

	db.InitDB(os.Getenv("DATABASE_URL"))
	if err := indexer.GlobalEngine.LoadFromDB(context.Background()); err != nil {
		logger.Log.Info("No existing persisted corpus loaded from DB", zap.Error(err))
	}
	indexer.GlobalEngine.StartPeriodicDBHydrator(context.Background(), 10*time.Second)

	handler := api.NewSearchHandler(indexer.GlobalEngine)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Post("/search", handler.Search)
	r.Get("/search", handler.Search)
	r.Post("/v1/search", handler.Search)
	r.Get("/v1/search", handler.Search)
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
