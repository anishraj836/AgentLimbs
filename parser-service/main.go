package main

import (
	"context"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/crawler-monorepo/common/kafka"
	"github.com/crawler-monorepo/common/logger"
	"github.com/crawler-monorepo/common/metrics"
	"github.com/crawler-monorepo/common/utils"
	"github.com/crawler-monorepo/parser-service/parser"
	ckafka "github.com/segmentio/kafka-go"
	"go.uber.org/zap"
)

func main() {
	// Initialize Logger
	logger.InitLogger(os.Getenv("ENV"))
	defer logger.Sync()

	logger.Log.Info("Starting Parser Service...")

	// Start Metrics Server
	metrics.InitMetricsServer("8083")

	// Kafka Setup
	rawBrokers := strings.Split(os.Getenv("KAFKA_BROKERS"), ",")
	var kafkaBrokers []string
	for _, b := range rawBrokers {
		if trimmed := strings.TrimSpace(b); trimmed != "" {
			kafkaBrokers = append(kafkaBrokers, trimmed)
		}
	}
	consumer := kafka.NewConsumer(kafkaBrokers, "parser-group", "downloaded_pages")
	producer := kafka.NewProducer(kafkaBrokers, "discovered_urls")
	defer consumer.Close()
	defer producer.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const concurrencyLimit = 20
	sem := make(chan struct{}, concurrencyLimit)
	var wg sync.WaitGroup
	offsetTracker := kafka.NewOffsetTracker()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			msg, err := consumer.ReadMessage(ctx)
			if err != nil {
				// Exit cleanly on context cancellation
				if ctx.Err() != nil {
					return
				}
				logger.Log.Error("Failed to read message", zap.Error(err))
				continue
			}

			sem <- struct{}{}
			wg.Add(1)
			go func(m ckafka.Message) {
				defer wg.Done()
				defer func() { <-sem }()

				sourceURL := string(m.Key)
				htmlContent := m.Value

				links, err := parser.ExtractLinks(sourceURL, htmlContent)
				if err != nil {
					logger.Log.Error("Failed to parse HTML", zap.String("url", sourceURL), zap.Error(err))
				} else {
					for _, link := range links {
						domain, err := utils.GetDomain(link)
						if err != nil {
							domain = "default"
						}
						if err := producer.Publish(ctx, []byte(domain), []byte(link)); err != nil {
							logger.Log.Error("Failed to publish discovered URL",
								zap.String("link", link), zap.Error(err))
							continue
						}
						metrics.URLsDiscovered.Inc()
					}
				}

				commitCtx := ctx
				if ctx.Err() != nil {
					var cancelCommit context.CancelFunc
					commitCtx, cancelCommit = context.WithTimeout(context.Background(), 5*time.Second)
					defer cancelCommit()
				}

				// Commit contiguous offset
				if err := offsetTracker.MarkCompleted(commitCtx, consumer, m); err != nil {
					logger.Log.Error("Failed to commit offset cleanly", zap.Error(err))
				}
			}(msg)
		}
	}()

	// Graceful Shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	logger.Log.Info("Shutting down Parser Service...")
	cancel()
	<-done
	wg.Wait()
	logger.Log.Info("Parser Service shutdown complete.")
}
