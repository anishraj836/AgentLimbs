package main

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/crawler-monorepo/common/db"
	"github.com/crawler-monorepo/common/kafka"
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

	rawBrokers := strings.Split(os.Getenv("KAFKA_BROKERS"), ",")
	var kafkaBrokers []string
	for _, b := range rawBrokers {
		if trimmed := strings.TrimSpace(b); trimmed != "" {
			kafkaBrokers = append(kafkaBrokers, trimmed)
		}
	}

	if len(kafkaBrokers) > 0 {
		consumer := kafka.NewConsumer(kafkaBrokers, "search-group", "index_updates")
		defer consumer.Close()

		go func() {
			ctx := context.Background()
			for {
				msg, err := consumer.ReadMessage(ctx)
				if err != nil {
					logger.Log.Error("Failed to read index_updates message", zap.Error(err))
					continue
				}

				if err := indexer.GlobalEngine.LoadFromDB(ctx); err != nil {
					logger.Log.Error("Failed to reload index from DB on index_updates event", zap.Error(err))
				} else {
					logger.Log.Info("Successfully reloaded index from DB via index_updates event")
				}

				if err := consumer.Commit(ctx, msg); err != nil {
					logger.Log.Error("Failed to commit offset for index_updates message", zap.Error(err))
				}
			}
		}()
	}

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

