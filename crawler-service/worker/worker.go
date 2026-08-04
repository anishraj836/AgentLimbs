package worker

import (
	"context"
	ckafka "github.com/crawler-monorepo/common/kafka"
	"github.com/crawler-monorepo/common/logger"
	"github.com/crawler-monorepo/common/metrics"
	"github.com/crawler-monorepo/common/redis"
	"github.com/crawler-monorepo/common/utils"
	"github.com/crawler-monorepo/crawler-service/httpclient"
	"github.com/crawler-monorepo/crawler-service/storage"
	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"
	"io"
	"strings"
	"sync"
	"time"
)

type CrawlerWorker struct {
	consumer         *ckafka.Consumer
	producer         *ckafka.Producer
	dlqProducer      *ckafka.Producer
	httpClient       *httpclient.Client
	storage          *storage.LocalStorage
	concurrencyLimit int
	wg               sync.WaitGroup
	offsetTracker    *ckafka.OffsetTracker
}

func NewCrawlerWorker(brokers []string, inTopic, outTopic, storageDir string, limit int) *CrawlerWorker {
	return &CrawlerWorker{
		consumer:         ckafka.NewConsumer(brokers, "crawler-group-1", inTopic),
		producer:         ckafka.NewProducer(brokers, outTopic),
		dlqProducer:      ckafka.NewProducer(brokers, "crawl_failed_dlq"),
		httpClient:       httpclient.NewClient(),
		storage:          storage.NewLocalStorage(storageDir),
		concurrencyLimit: limit,
		offsetTracker:    ckafka.NewOffsetTracker(),
	}
}

func (w *CrawlerWorker) Start(ctx context.Context) {
	sem := make(chan struct{}, w.concurrencyLimit)

	logger.Log.Info("Starting Crawler Worker loop...")

	for {
		msg, err := w.consumer.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				logger.Log.Info("Crawler worker stopping due to context cancellation")
				w.wg.Wait()
				return
			}
			logger.Log.Error("Failed to read message from Kafka", zap.Error(err))
			continue
		}

		sem <- struct{}{}
		w.wg.Add(1)
		go func(url string, m kafka.Message) {
			defer w.wg.Done()
			defer func() { <-sem }()

			w.processURL(ctx, url)

			commitCtx := ctx
			if ctx.Err() != nil {
				var cancel context.CancelFunc
				commitCtx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
			}

			if err := w.offsetTracker.MarkCompleted(commitCtx, w.consumer, m); err != nil {
				logger.Log.Error("Failed to commit offset cleanly", zap.Error(err))
			}
		}(string(msg.Value), msg)
	}
}

func (w *CrawlerWorker) Wait() {
	w.wg.Wait()
}

func (w *CrawlerWorker) enforcePoliteness(ctx context.Context, domain string) {
	if redis.Client == nil || domain == "unknown" || domain == "" {
		return
	}
	key := "crawler:rate:" + domain
	for {
		ok, err := redis.Client.SetNX(ctx, key, "1", 500*time.Millisecond).Result()
		if err == nil && ok {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (w *CrawlerWorker) processURL(ctx context.Context, url string) {
	domain, err := utils.GetDomain(url)
	if err != nil {
		domain = "unknown"
	}

	w.enforcePoliteness(ctx, domain)

	startTime := time.Now()
	logger.Log.Info("Fetching URL", zap.String("url", url))

	result, err := w.httpClient.Fetch(ctx, url)
	if err != nil {
		logger.Log.Error("Failed to fetch URL, routing to DLQ", zap.String("url", url), zap.Error(err))
		metrics.CrawlErrors.WithLabelValues("fetch_error").Inc()
		if dlqErr := w.dlqProducer.Publish(ctx, []byte(domain), []byte(url)); dlqErr != nil {
			logger.Log.Error("Failed to publish to DLQ", zap.String("url", url), zap.Error(dlqErr))
		}
		return
	}
	defer result.Response.Body.Close()

	latency := time.Since(startTime).Seconds()
	metrics.HttpLatency.WithLabelValues(domain).Observe(latency)

	finalURL := result.FinalURL

	if result.Response.StatusCode == 200 {
		contentType := strings.ToLower(result.Response.Header.Get("Content-Type"))
		isHTML := strings.Contains(contentType, "text/html") ||
			strings.Contains(contentType, "application/xhtml+xml") ||
			contentType == ""

		const maxBodySize = 10 * 1024 * 1024 // 10MB
		limitedBody := io.LimitReader(result.Response.Body, maxBodySize)
		bodyBytes, err := io.ReadAll(limitedBody)
		if err != nil {
			logger.Log.Error("Failed to read body", zap.Error(err))
			return
		}

		savedPath, err := w.storage.Save(finalURL, bodyBytes)
		if err != nil {
			logger.Log.Error("Failed to save content", zap.Error(err))
			return
		}

		logger.Log.Info("Successfully crawled and stored",
			zap.String("original_url", url),
			zap.String("final_url", finalURL),
			zap.String("content_type", contentType),
			zap.String("path", savedPath),
		)
		metrics.PagesCrawled.Inc()

		if isHTML {
			if err := w.producer.Publish(ctx, []byte(finalURL), bodyBytes); err != nil {
				logger.Log.Error("Failed to publish to parser queue", zap.String("url", finalURL), zap.Error(err))
				metrics.CrawlErrors.WithLabelValues("publish_error").Inc()
			}
		} else {
			logger.Log.Info("Skipping parser publishing for non-HTML content",
				zap.String("url", finalURL),
				zap.String("content_type", contentType),
			)
		}
	} else {
		logger.Log.Warn("Non-200 HTTP status response",
			zap.String("url", url),
			zap.Int("status_code", result.Response.StatusCode),
		)
		metrics.CrawlErrors.WithLabelValues("http_non_200").Inc()
	}
}

func (w *CrawlerWorker) Close() {
	w.consumer.Close()
	w.producer.Close()
	w.dlqProducer.Close()
}
