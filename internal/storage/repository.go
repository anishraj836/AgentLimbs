package storage

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/crawler-monorepo/common/utils"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool maintains backwards compatibility for any callers accessing storage.Pool directly.
var Pool *pgxpool.Pool

var (
	repoMu sync.Mutex
)

// InitDB initializes the database and updates the global store.
// If databaseURL is provided, it connects to PostgreSQL; otherwise it initializes file-fallback storage.
func InitDB(databaseURL string) {
	repoMu.Lock()
	defer repoMu.Unlock()

	if databaseURL == "" {
		log.Printf("[Storage] No database URL provided; operating in file-fallback mode")
		Pool = nil
		SetGlobalStore(NewFileStore(""))
		return
	}

	pgStore, err := NewPostgresStore(databaseURL)
	if err != nil || pgStore == nil {
		log.Printf("[Storage] PostgreSQL initialization failed: %v; operating in file-fallback mode", err)
		Pool = nil
		SetGlobalStore(NewFileStore(""))
		return
	}

	Pool = pgStore.Pool()
	SetGlobalStore(pgStore)
}

func getPool() *pgxpool.Pool {
	repoMu.Lock()
	defer repoMu.Unlock()
	return Pool
}

// PingDB checks if the database is accessible, returning nil if operating in fallback mode (Pool == nil).
func PingDB(ctx context.Context) error {
	p := getPool()
	if p == nil {
		return nil
	}
	return p.Ping(ctx)
}

// InitTables initializes database tables if PostgreSQL is connected.
func InitTables(ctx context.Context) error {
	p := getPool()
	if p == nil {
		return nil
	}
	query := `
	CREATE TABLE IF NOT EXISTS crawled_pages (
		id SERIAL PRIMARY KEY,
		url TEXT UNIQUE NOT NULL,
		title TEXT NOT NULL,
		clean_body TEXT NOT NULL,
		total_tokens INT NOT NULL DEFAULT 0,
		source_type TEXT NOT NULL DEFAULT 'web_crawled',
		source_url TEXT,
		created_at TIMESTAMPTZ DEFAULT NOW(),
		expires_at TIMESTAMPTZ
	);
	CREATE INDEX IF NOT EXISTS idx_crawled_pages_url ON crawled_pages(url);
	CREATE INDEX IF NOT EXISTS idx_crawled_pages_expires_at ON crawled_pages(expires_at);
	`
	_, err := p.Exec(ctx, query)
	return err
}

// SaveCrawledDocument saves a document without expiration.
func SaveCrawledDocument(ctx context.Context, url, title, cleanBody string, totalTokens int, sourceType, sourceURL string) error {
	return SaveCrawledDocumentWithTTL(ctx, url, title, cleanBody, totalTokens, sourceType, sourceURL, 0)
}

// SaveCrawledDocumentWithTTL saves a document with a TTL expiration duration.
func SaveCrawledDocumentWithTTL(ctx context.Context, url, title, cleanBody string, totalTokens int, sourceType, sourceURL string, ttl time.Duration) error {
	doc := &CrawledDocument{
		URL:         url,
		Title:       title,
		CleanBody:   cleanBody,
		TotalTokens: totalTokens,
		SourceType:  sourceType,
		SourceURL:   sourceURL,
	}
	return GetGlobalStore().Save(ctx, doc, ttl)
}

// GetCrawledDocuments returns all active stored documents.
func GetCrawledDocuments(ctx context.Context) ([]CrawledDocument, error) {
	return GetGlobalStore().List(ctx, 0, 0)
}

// GetCrawledDocumentByURL retrieves a document by URL or alias.
func GetCrawledDocumentByURL(ctx context.Context, targetURL string) (*CrawledDocument, error) {
	return GetGlobalStore().GetByURL(ctx, targetURL)
}

// DeleteExpiredDocuments cleans up any expired documents across the active storage backend.
func DeleteExpiredDocuments(ctx context.Context) (int64, error) {
	return GetGlobalStore().DeleteExpired(ctx)
}

// SaveURLAlias stores an alias mapping from aliasURL to canonicalURL.
func SaveURLAlias(ctx context.Context, aliasURL, canonicalURL string) error {
	return GetGlobalStore().SaveAlias(ctx, aliasURL, canonicalURL)
}

// GetURLAlias resolves an alias URL to its canonical URL if registered.
func GetURLAlias(aliasURL string) string {
	return GetGlobalStore().GetAlias(context.Background(), aliasURL)
}

// GetStoragePath returns the local fallback file storage path.
func GetStoragePath() string {
	dir := "data"
	_ = os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "crawled_pages.json")
}

// CloseDB closes the active database connection pool.
func CloseDB() {
	repoMu.Lock()
	defer repoMu.Unlock()
	if Pool != nil {
		Pool.Close()
		Pool = nil
	}
	if store := GetGlobalStore(); store != nil {
		if store.DriverName() != "postgres" {
			_ = store.Close()
		}
	}
}

// LocalStorage manages raw HTML gzip file persistence.
type LocalStorage struct {
	baseDir string
}

// NewLocalStorage creates a LocalStorage instance rooted at baseDir.
func NewLocalStorage(baseDir string) *LocalStorage {
	return &LocalStorage{baseDir: baseDir}
}

// Save saves gzipped raw HTML bytes atomically under baseDir/YYYY-MM-DD/domain/hash.html.gz.
func (s *LocalStorage) Save(url string, content []byte) (string, error) {
	domain, err := utils.GetDomain(url)
	if err != nil {
		return "", err
	}

	safeDomain := filepath.Base(filepath.Clean(domain))
	if safeDomain == "." || safeDomain == "/" || safeDomain == "" {
		safeDomain = "unknown"
	}

	hash := utils.HashURL(url)
	dateStr := time.Now().Format("2006-01-02")

	dirPath := filepath.Join(s.baseDir, dateStr, safeDomain)
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return "", err
	}

	filePath := filepath.Join(dirPath, hash+".html.gz")

	tmpFile, err := os.CreateTemp(dirPath, ".tmp-gzip-*")
	if err != nil {
		return "", err
	}
	tmpName := tmpFile.Name()
	defer os.Remove(tmpName)

	gw := gzip.NewWriter(tmpFile)
	_, copyErr := io.Copy(gw, bytes.NewReader(content))
	gwCloseErr := gw.Close()
	syncErr := tmpFile.Sync()
	fileCloseErr := tmpFile.Close()

	if copyErr != nil || gwCloseErr != nil || syncErr != nil || fileCloseErr != nil {
		if copyErr != nil {
			return "", copyErr
		}
		if gwCloseErr != nil {
			return "", gwCloseErr
		}
		if syncErr != nil {
			return "", syncErr
		}
		return "", fileCloseErr
	}

	if err := os.Chmod(tmpName, 0644); err != nil {
		return "", err
	}

	if err := os.Rename(tmpName, filePath); err != nil {
		return "", err
	}

	return filePath, nil
}
