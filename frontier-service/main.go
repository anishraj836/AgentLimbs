package main

import (
	"context"
	"github.com/crawler-monorepo/common/db"
	"github.com/crawler-monorepo/common/logger"
	"github.com/crawler-monorepo/common/metrics"
	"github.com/crawler-monorepo/common/redis"
	"github.com/crawler-monorepo/frontier-service/api"
	"github.com/crawler-monorepo/frontier-service/service"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	// Initialize Logger
	logger.InitLogger(os.Getenv("ENV"))
	defer logger.Sync()

	logger.Log.Info("Starting Frontier Service...")

	// Initialize Dependencies
	redis.InitRedis(os.Getenv("REDIS_ADDR"), "", 0)
	db.InitDB(os.Getenv("DATABASE_URL"))
	defer db.CloseDB()

	// Start Metrics Server
	metrics.InitMetricsServer("8081")

	// Initialize Service
	rawBrokers := strings.Split(os.Getenv("KAFKA_BROKERS"), ",")
	var kafkaBrokers []string
	for _, b := range rawBrokers {
		if trimmed := strings.TrimSpace(b); trimmed != "" {
			kafkaBrokers = append(kafkaBrokers, trimmed)
		}
	}
	frontier := service.NewFrontierService(kafkaBrokers, "crawl_requests")
	defer frontier.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start consuming discovered URLs from Parser Service
	go frontier.StartDiscoveredURLConsumer(ctx)

	// Setup Router
	r := chi.NewRouter()
	handler := api.NewHandler(frontier)
	handler.RegisterRoutes(r)

	// Start Server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: r,
	}

	go func() {
		logger.Log.Info("Frontier Service API listening", zap.String("port", port))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Log.Fatal("Server failed", zap.Error(err))
		}
	}()

	// Graceful Shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	logger.Log.Info("Shutting down Frontier Service...")

	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Log.Error("Server forced to shutdown", zap.Error(err))
	}
}
