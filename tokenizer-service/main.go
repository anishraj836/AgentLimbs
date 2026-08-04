package main

import (
	"context"
	"encoding/json"
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
	"github.com/crawler-monorepo/tokenizer-service/tokenizer"
	ckafka "github.com/segmentio/kafka-go"
	"go.uber.org/zap"
)

func main() {
	logger.InitLogger(os.Getenv("ENV"))
	defer logger.Sync()

	logger.Log.Info("Starting Tokenizer Service...")
	metrics.InitMetricsServer("8085")

	rawBrokers := strings.Split(os.Getenv("KAFKA_BROKERS"), ",")
	var kafkaBrokers []string
	for _, b := range rawBrokers {
		if trimmed := strings.TrimSpace(b); trimmed != "" {
			kafkaBrokers = append(kafkaBrokers, trimmed)
		}
	}

	consumer := kafka.NewConsumer(kafkaBrokers, "tokenizer-group", "parsed_documents")
	producer := kafka.NewProducer(kafkaBrokers, "tokenized_documents")
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
				logger.Log.Error("Failed to read parsed document message", zap.Error(err))
				continue
			}

			sem <- struct{}{}
			wg.Add(1)
			go func(m ckafka.Message) {
				defer wg.Done()
				defer func() { <-sem }()

				var doc processor.CleanDocument
				if err := json.Unmarshal(m.Value, &doc); err != nil {
					logger.Log.Error("Failed to unmarshal clean document", zap.Error(err))
				} else {
					tokenizedDoc := tokenizer.TokenizePipeline(doc.URL, doc.Title, doc.Body)
					jsonBytes, err := tokenizedDoc.SerializeJSON()
					if err == nil {
						if err := producer.Publish(ctx, []byte(doc.URL), jsonBytes); err != nil {
							logger.Log.Error("Failed to publish tokenized document", zap.String("url", doc.URL), zap.Error(err))
						} else {
							logger.Log.Info("Tokenized document successfully",
								zap.String("url", doc.URL),
								zap.Int("tokens", tokenizedDoc.TotalTokens))
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
	logger.Log.Info("Shutting down Tokenizer Service...")
	cancel()
	<-done
	wg.Wait()
	logger.Log.Info("Tokenizer Service shutdown complete.")
}
