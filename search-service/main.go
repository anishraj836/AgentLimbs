package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/crawler-monorepo/common/config"
	"github.com/crawler-monorepo/common/kafka"
	"github.com/crawler-monorepo/common/logger"
	appMiddleware "github.com/crawler-monorepo/common/middleware"
	"github.com/crawler-monorepo/common/redis"
	"github.com/crawler-monorepo/common/tracing"
	"github.com/crawler-monorepo/internal/auth"
	"github.com/crawler-monorepo/internal/index"
	"github.com/crawler-monorepo/internal/storage"
	"github.com/crawler-monorepo/search-service/api"
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
		logger.Log.Info("No existing persisted corpus loaded from DB", zap.Error(err))
	}

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
			for {
				msg, err := consumer.ReadMessage(ctx)
				if err != nil {
					if ctx.Err() != nil {
						logger.Log.Info("Kafka consumer shutting down")
						break
					}
					logger.Log.Error("Failed to read index_updates message", zap.Error(err))
					continue
				}

				targetURL := string(msg.Key)
				if targetURL == "" || targetURL == "indexed" {
					targetURL = string(msg.Value)
				}

				if targetURL != "" && targetURL != "indexed" {
					if err := index.GlobalEngine.IndexDocumentIncrementalByURL(ctx, targetURL); err != nil {
						logger.Log.Error("Failed to incrementally index URL on index_updates event", zap.String("url", targetURL), zap.Error(err))
					} else {
						logger.Log.Info("Successfully incrementally indexed document via index_updates event", zap.String("url", targetURL))
					}
				}

				if err := consumer.Commit(ctx, msg); err != nil {
					logger.Log.Error("Failed to commit offset for index_updates message", zap.Error(err))
				}
			}
		}()
	}

	handler := api.NewSearchHandler(index.GlobalEngine)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(appMiddleware.RequestIDMiddleware)
	r.Use(tracing.TracingMiddleware)
	r.Use(auth.TenantMiddleware)

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
			logger.Log.Fatal("Search Service failed", zap.Error(err))
		}
	}()

	<-ctx.Done()
	logger.Log.Info("Shutting down Search Service gracefully...")
	
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Log.Error("Search Service forced to shutdown", zap.Error(err))
	}
}
