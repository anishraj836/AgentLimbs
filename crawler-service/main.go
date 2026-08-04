package main

import (
	"context"
	"github.com/crawler-monorepo/common/logger"
	"github.com/crawler-monorepo/common/metrics"
	"github.com/crawler-monorepo/common/redis"
	"github.com/crawler-monorepo/crawler-service/worker"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

func main() {
	// Initialize Logger
	logger.InitLogger(os.Getenv("ENV"))
	defer logger.Sync()

	logger.Log.Info("Starting Crawler Service...")

	// Initialize Redis for per-domain crawl rate limiting.
	redis.InitRedis(os.Getenv("REDIS_ADDR"), "", 0)

	// Start Metrics Server
	metrics.InitMetricsServer("8082")

	// Start Worker
	rawBrokers := strings.Split(os.Getenv("KAFKA_BROKERS"), ",")
	var kafkaBrokers []string
	for _, b := range rawBrokers {
		if trimmed := strings.TrimSpace(b); trimmed != "" {
			kafkaBrokers = append(kafkaBrokers, trimmed)
		}
	}
	w := worker.NewCrawlerWorker(kafkaBrokers, "crawl_requests", "downloaded_pages", "/data/raw_html", 50)
	defer w.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go w.Start(ctx)

	// Graceful Shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	logger.Log.Info("Shutting down Crawler Service...")
	cancel()
	w.Wait()
	logger.Log.Info("Crawler Service shutdown complete.")
}
