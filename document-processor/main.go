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
	"github.com/crawler-monorepo/document-processor/processor"
	ckafka "github.com/segmentio/kafka-go"
	"go.uber.org/zap"
)

func main() {
	logger.InitLogger(os.Getenv("ENV"))
	defer logger.Sync()

	logger.Log.Info("Starting Document Processor Service...")
	metrics.InitMetricsServer("8084")

	rawBrokers := strings.Split(os.Getenv("KAFKA_BROKERS"), ",")
	var kafkaBrokers []string
	for _, b := range rawBrokers {
		if trimmed := strings.TrimSpace(b); trimmed != "" {
			kafkaBrokers = append(kafkaBrokers, trimmed)
		}
	}

	consumer := kafka.NewConsumer(kafkaBrokers, "doc-processor-group", "downloaded_pages")
	producer := kafka.NewProducer(kafkaBrokers, "parsed_documents")
	defer consumer.Close()
	defer producer.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const concurrencyLimit = 10
	sem := make(chan struct{}, concurrencyLimit)
	var wg sync.WaitGroup
	offsetTracker := kafka.NewOffsetTracker()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			msg, err := consumer.ReadMessage(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				logger.Log.Error("Failed to read downloaded page message", zap.Error(err))
				continue
			}

			sem <- struct{}{}
			wg.Add(1)
			go func(m ckafka.Message) {
				defer wg.Done()
				defer func() { <-sem }()

				sourceURL := string(m.Key)
				rawHTML := m.Value

				cleanDoc, err := processor.ProcessRawHTML(sourceURL, rawHTML)
				if err != nil {
					logger.Log.Error("Failed to clean document HTML", zap.String("url", sourceURL), zap.Error(err))
				} else {
					jsonBytes, err := cleanDoc.SerializeJSON()
					if err == nil {
						if err := producer.Publish(ctx, []byte(sourceURL), jsonBytes); err != nil {
							logger.Log.Error("Failed to publish clean document", zap.String("url", sourceURL), zap.Error(err))
						} else {
							logger.Log.Info("Processed and published clean document", zap.String("url", sourceURL))
						}
					}
				}

				commitCtx := ctx
				if ctx.Err() != nil {
					var cancelCommit context.CancelFunc
					commitCtx, cancelCommit = context.WithTimeout(context.Background(), 5*time.Second)
					defer cancelCommit()
				}
				offsetTracker.MarkCompleted(commitCtx, consumer, m)
			}(msg)
		}
	}()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	logger.Log.Info("Shutting down Document Processor Service...")
	cancel()
	<-done
	wg.Wait()
	logger.Log.Info("Document Processor Service shutdown complete.")
}
