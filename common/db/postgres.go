package db

import (
	"context"
	"time"

	"github.com/crawler-monorepo/internal/storage"
)

type CrawledDocument = storage.CrawledDocument

var Pool = storage.Pool

func InitDB(databaseURL string) {
	storage.InitDB(databaseURL)
	Pool = storage.Pool
}

func InitTables(ctx context.Context) error {
	return storage.InitTables(ctx)
}

func SaveCrawledDocument(ctx context.Context, url, title, cleanBody string, totalTokens int, sourceType, sourceURL string) error {
	return storage.SaveCrawledDocument(ctx, url, title, cleanBody, totalTokens, sourceType, sourceURL)
}

func SaveCrawledDocumentWithTTL(ctx context.Context, url, title, cleanBody string, totalTokens int, sourceType, sourceURL string, ttl time.Duration) error {
	return storage.SaveCrawledDocumentWithTTL(ctx, url, title, cleanBody, totalTokens, sourceType, sourceURL, ttl)
}

func GetCrawledDocuments(ctx context.Context) ([]CrawledDocument, error) {
	return storage.GetCrawledDocuments(ctx)
}

func GetCrawledDocumentByURL(ctx context.Context, targetURL string) (*CrawledDocument, error) {
	return storage.GetCrawledDocumentByURL(ctx, targetURL)
}

func DeleteExpiredDocuments(ctx context.Context) (int64, error) {
	return storage.DeleteExpiredDocuments(ctx)
}

func CloseDB() {
	storage.CloseDB()
}

func getStoragePath() string {
	return storage.GetStoragePath()
}
