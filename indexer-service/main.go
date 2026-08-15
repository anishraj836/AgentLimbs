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
	"github.com/crawler-monorepo/internal/index"
	"github.com/crawler-monorepo/internal/storage"
	ckafka "github.com/segmentio/kafka-go"
	"go.uber.org/zap"
)

type TokenizedDocument struct {
	URL           string           `json:"url"`
	Title         string           `json:"title"`
	CleanBody     string           `json:"clean_body"`
	TermPositions map[string][]int `json:"term_positions"`
	TotalTokens   int              `json:"total_tokens"`
}

func main() {
	logger.InitLogger(os.Getenv("ENV"))
	defer logger.Sync()

	storage.InitDB(os.Getenv("DATABASE_URL"))
	defer storage.CloseDB()

	logger.Log.Info("Starting Indexer Service...")
	metrics.InitMetricsServer("8086")

	rawBrokers := strings.Split(os.Getenv("KAFKA_BROKERS"), ",")
	var kafkaBrokers []string
	for _, b := range rawBrokers {
		if trimmed := strings.TrimSpace(b); trimmed != "" {
			kafkaBrokers = append(kafkaBrokers, trimmed)
		}
	}

	consumer := kafka.NewConsumer(kafkaBrokers, "indexer-group", "tokenized_documents")
	producer := kafka.NewProducer(kafkaBrokers, "index_updates")
	defer consumer.Close()
	defer producer.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start background TTL janitor routine to purge expired documents every 15 minutes
	index.GlobalEngine.StartTTLJanitor(ctx, 15*time.Minute)

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
				logger.Log.Error("Failed to read tokenized document message", zap.Error(err))
				continue
			}

			select {
			case <-ctx.Done():
				return
			case sem <- struct{}{}:
			}
			wg.Add(1)
			offsetTracker.MarkStarted(msg)
			go func(m ckafka.Message) {
				defer wg.Done()
				defer func() { <-sem }()

				var succeeded bool
				defer func() {
					if r := recover(); r != nil {
						logger.Log.Error("Panic isolated in indexer worker", zap.Any("recover", r))
					}
					commitCtx := ctx
					if ctx.Err() != nil {
						var cancelCommit context.CancelFunc
						commitCtx, cancelCommit = context.WithTimeout(context.Background(), 5*time.Second)
						defer cancelCommit()
					}
					if succeeded {
						if err := offsetTracker.MarkCompleted(commitCtx, consumer, m); err != nil {
							logger.Log.Error("Failed to mark offset completed", zap.Error(err), zap.Int64("offset", m.Offset))
						}
					} else {
						if err := offsetTracker.MarkFailed(commitCtx, consumer, m); err != nil {
							logger.Log.Error("Failed to mark offset failed", zap.Error(err), zap.Int64("offset", m.Offset))
						}
					}
				}()

				var tokenizedDoc TokenizedDocument
				if err := json.Unmarshal(m.Value, &tokenizedDoc); err != nil {
					logger.Log.Error("Failed to unmarshal tokenized document", zap.Error(err))
					return
				}

				index.GlobalEngine.IndexDocument(
					tokenizedDoc.URL,
					tokenizedDoc.Title,
					tokenizedDoc.CleanBody,
					tokenizedDoc.TermPositions,
					tokenizedDoc.TotalTokens,
				)
				logger.Log.Info("Successfully indexed document",
					zap.String("url", tokenizedDoc.URL),
					zap.Int("unique_terms", len(tokenizedDoc.TermPositions)))

				// Publish index update notification
				pubCtx := ctx
				if ctx.Err() != nil {
					var cancelPub context.CancelFunc
					pubCtx, cancelPub = context.WithTimeout(context.Background(), 5*time.Second)
					defer cancelPub()
				}
				producer.Publish(pubCtx, []byte(tokenizedDoc.URL), []byte("indexed"))
				succeeded = true
			}(msg)
		}
	}()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	logger.Log.Info("Shutting down Indexer Service...")
	cancel()
	<-done
	wg.Wait()
	logger.Log.Info("Indexer Service shutdown complete.")
}
