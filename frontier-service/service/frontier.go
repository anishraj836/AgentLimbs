package service

import (
	"context"
	"time"

	"github.com/crawler-monorepo/common/kafka"
	"github.com/crawler-monorepo/common/logger"
	"github.com/crawler-monorepo/common/redis"
	"github.com/crawler-monorepo/common/utils"
	"go.uber.org/zap"
)

type FrontierService struct {
	kafkaProducer      *kafka.Producer
	discoveredConsumer *kafka.Consumer
	dedup              *redis.SetBasedDedup
}

func NewFrontierService(brokers []string, outTopic string) *FrontierService {
	return &FrontierService{
		kafkaProducer:      kafka.NewProducer(brokers, outTopic),
		discoveredConsumer: kafka.NewConsumer(brokers, "frontier-group", "discovered_urls"),
		dedup:              redis.NewSetBasedDedup("crawler:dedup:urls"),
	}
}

// StartDiscoveredURLConsumer listens to the 'discovered_urls' Kafka topic
// produced by the Parser Service and queues unique URLs into 'crawl_requests'.
// This completes the distributed crawling feedback loop.
func (f *FrontierService) StartDiscoveredURLConsumer(ctx context.Context) {
	logger.Log.Info("Starting Frontier discovered URLs consumer loop...")
	defer func() {
		if r := recover(); r != nil {
			logger.Log.Error("Panic recovered in discovered URL consumer", zap.Any("panic", r))
		}
	}()
	for {
		msg, err := f.discoveredConsumer.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				logger.Log.Info("Frontier discovered URLs consumer stopping due to context cancellation")
				return
			}
			logger.Log.Error("Failed to read discovered URL message", zap.Error(err))
			continue
		}

		rawURL := string(msg.Value)
		f.processDiscoveredURL(ctx, rawURL)

		commitCtx := ctx
		if ctx.Err() != nil {
			var cancel context.CancelFunc
			commitCtx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
		}

		if err := f.discoveredConsumer.Commit(commitCtx, msg); err != nil {
			logger.Log.Error("Failed to commit offset in discovered URL consumer", zap.Error(err))
		}
	}
}

func (f *FrontierService) processDiscoveredURL(ctx context.Context, rawURL string) {
	normURL, err := utils.NormalizeURL(rawURL)
	if err != nil {
		logger.Log.Debug("Invalid discovered URL", zap.String("url", rawURL), zap.Error(err))
		return
	}

	exists, err := f.dedup.Exists(ctx, normURL)
	if err != nil {
		logger.Log.Error("Failed to check deduplication set for discovered URL", zap.Error(err))
		return
	}
	if exists {
		return
	}

	domain, err := utils.GetDomain(normURL)
	if err != nil {
		domain = "default"
	}

	if err := f.kafkaProducer.Publish(ctx, []byte(domain), []byte(normURL)); err != nil {
		logger.Log.Error("Failed to publish discovered URL to crawl_requests", zap.String("url", normURL), zap.Error(err))
		return
	}

	if _, err := f.dedup.Add(ctx, normURL); err != nil {
		logger.Log.Error("Failed to add discovered URL to deduplication set", zap.String("url", normURL), zap.Error(err))
	} else {
		logger.Log.Info("Queued discovered URL for crawling", zap.String("url", normURL))
	}
}

// AddSeeds normalizes URLs, checks for duplicates, and queues them in Kafka.
// The order is: check dedup → publish to Kafka → mark in dedup.
// If any single URL encounters an error, it is logged and processing continues for the remaining URLs.
func (f *FrontierService) AddSeeds(ctx context.Context, urls []string) (int, error) {
	addedCount := 0
	for _, rawURL := range urls {
		normURL, err := utils.NormalizeURL(rawURL)
		if err != nil {
			logger.Log.Warn("Invalid seed URL", zap.String("url", rawURL), zap.Error(err))
			continue
		}

		// Check if URL was already seen
		exists, err := f.dedup.Exists(ctx, normURL)
		if err != nil {
			logger.Log.Error("Failed to check deduplication set", zap.String("url", normURL), zap.Error(err))
			continue
		}
		if exists {
			logger.Log.Debug("URL already seen", zap.String("url", normURL))
			continue
		}

		domain, err := utils.GetDomain(normURL)
		if err != nil {
			domain = "default"
		}

		// Publish to Kafka FIRST (partition key = domain)
		err = f.kafkaProducer.Publish(ctx, []byte(domain), []byte(normURL))
		if err != nil {
			logger.Log.Error("Failed to publish seed URL to Kafka", zap.String("url", normURL), zap.Error(err))
			continue
		}

		// Only mark as "seen" AFTER Kafka publish succeeded.
		if _, err := f.dedup.Add(ctx, normURL); err != nil {
			logger.Log.Error("Failed to add to deduplication set (URL already queued)", zap.Error(err))
		}

		addedCount++
		logger.Log.Info("Added new seed URL", zap.String("url", normURL))
	}
	return addedCount, nil
}

func (f *FrontierService) Close() {
	if err := f.discoveredConsumer.Close(); err != nil {
		logger.Log.Error("Error closing discovered URL consumer", zap.Error(err))
	}
	if err := f.kafkaProducer.Close(); err != nil {
		logger.Log.Error("Error closing Kafka producer", zap.Error(err))
	}
}
