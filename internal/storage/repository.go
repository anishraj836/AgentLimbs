package storage

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/crawler-monorepo/common/utils"
	"github.com/jackc/pgx/v5/pgxpool"
)

var Pool *pgxpool.Pool

type CrawledDocument struct {
	ID          int        `json:"id"`
	URL         string     `json:"url"`
	Title       string     `json:"title"`
	CleanBody   string     `json:"clean_body"`
	TotalTokens int        `json:"total_tokens"`
	SourceType  string     `json:"source_type"`
	SourceURL   string     `json:"source_url"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

func InitDB(databaseURL string) {
	if databaseURL == "" {
		databaseURL = "postgres://crawler:crawler_password@localhost:5432/crawler_db"
	}
	var err error
	Pool, err = pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		return
	}
	_ = InitTables(context.Background())
}

func InitTables(ctx context.Context) error {
	if Pool == nil {
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
	);`
	_, err := Pool.Exec(ctx, query)
	return err
}

func SaveCrawledDocument(ctx context.Context, url, title, cleanBody string, totalTokens int, sourceType, sourceURL string) error {
	return SaveCrawledDocumentWithTTL(ctx, url, title, cleanBody, totalTokens, sourceType, sourceURL, 0)
}

func SaveCrawledDocumentWithTTL(ctx context.Context, url, title, cleanBody string, totalTokens int, sourceType, sourceURL string, ttl time.Duration) error {
	if sourceType == "" {
		sourceType = "web_crawled"
	}
	if sourceURL == "" {
		sourceURL = url
	}

	var expiresAt *time.Time
	if ttl > 0 {
		t := time.Now().Add(ttl)
		expiresAt = &t
	}

	if Pool != nil {
		query := `
		INSERT INTO crawled_pages (url, title, clean_body, total_tokens, source_type, source_url, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (url) DO UPDATE
		SET title = EXCLUDED.title, clean_body = EXCLUDED.clean_body, total_tokens = EXCLUDED.total_tokens, source_type = EXCLUDED.source_type, source_url = EXCLUDED.source_url, expires_at = EXCLUDED.expires_at;`
		_, err := Pool.Exec(ctx, query, url, title, cleanBody, totalTokens, sourceType, sourceURL, expiresAt)
		if err == nil {
			return nil
		}
	}

	return saveToFileFallback(url, title, cleanBody, totalTokens, sourceType, sourceURL, expiresAt)
}

func GetCrawledDocuments(ctx context.Context) ([]CrawledDocument, error) {
	if Pool != nil {
		query := `SELECT id, url, title, clean_body, total_tokens, COALESCE(source_type, 'web_crawled'), COALESCE(source_url, url), expires_at FROM crawled_pages WHERE expires_at IS NULL OR expires_at > NOW() ORDER BY id ASC;`
		rows, err := Pool.Query(ctx, query)
		if err == nil {
			defer rows.Close()
			var docs []CrawledDocument
			for rows.Next() {
				var doc CrawledDocument
				if err := rows.Scan(&doc.ID, &doc.URL, &doc.Title, &doc.CleanBody, &doc.TotalTokens, &doc.SourceType, &doc.SourceURL, &doc.ExpiresAt); err == nil {
					docs = append(docs, doc)
				}
			}
			return docs, nil
		}
	}

	return getFromFileFallback()
}

func DeleteExpiredDocuments(ctx context.Context) (int64, error) {
	var totalDeleted int64
	if Pool != nil {
		query := `DELETE FROM crawled_pages WHERE expires_at IS NOT NULL AND expires_at <= NOW();`
		cmdTag, err := Pool.Exec(ctx, query)
		if err != nil {
			return 0, err
		}
		totalDeleted += cmdTag.RowsAffected()
	}

	fileDeleted, err := deleteExpiredFromFileFallback()
	if err != nil {
		return totalDeleted, err
	}
	totalDeleted += fileDeleted

	return totalDeleted, nil
}

var fallbackMu sync.Mutex

func GetStoragePath() string {
	dir := "data"
	_ = os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "crawled_pages.json")
}

func getStoragePath() string {
	return GetStoragePath()
}

func saveToFileFallback(url, title, cleanBody string, totalTokens int, sourceType, sourceURL string, expiresAt *time.Time) error {
	fallbackMu.Lock()
	defer fallbackMu.Unlock()

	filePath := getStoragePath()
	var docs []CrawledDocument

	if data, err := os.ReadFile(filePath); err == nil {
		_ = json.Unmarshal(data, &docs)
	}

	found := false
	for i, d := range docs {
		if d.URL == url {
			docs[i].Title = title
			docs[i].CleanBody = cleanBody
			docs[i].TotalTokens = totalTokens
			docs[i].SourceType = sourceType
			docs[i].SourceURL = sourceURL
			docs[i].ExpiresAt = expiresAt
			found = true
			break
		}
	}

	if !found {
		docID := len(docs) + 1
		docs = append(docs, CrawledDocument{
			ID:          docID,
			URL:         url,
			Title:       title,
			CleanBody:   cleanBody,
			TotalTokens: totalTokens,
			SourceType:  sourceType,
			SourceURL:   sourceURL,
			ExpiresAt:   expiresAt,
		})
	}

	data, err := json.MarshalIndent(docs, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filePath, data, 0644)
}

func getFromFileFallback() ([]CrawledDocument, error) {
	fallbackMu.Lock()
	defer fallbackMu.Unlock()

	filePath := getStoragePath()
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, nil
	}

	var docs []CrawledDocument
	if err := json.Unmarshal(data, &docs); err != nil {
		return nil, err
	}

	now := time.Now()
	var validDocs []CrawledDocument
	for _, d := range docs {
		if d.ExpiresAt != nil && !d.ExpiresAt.IsZero() && d.ExpiresAt.Before(now) {
			continue
		}
		validDocs = append(validDocs, d)
	}

	return validDocs, nil
}

func deleteExpiredFromFileFallback() (int64, error) {
	fallbackMu.Lock()
	defer fallbackMu.Unlock()

	filePath := getStoragePath()
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	var docs []CrawledDocument
	if err := json.Unmarshal(data, &docs); err != nil {
		return 0, err
	}

	now := time.Now()
	var validDocs []CrawledDocument
	var deletedCount int64

	for _, d := range docs {
		if d.ExpiresAt != nil && !d.ExpiresAt.IsZero() && !d.ExpiresAt.After(now) {
			deletedCount++
		} else {
			validDocs = append(validDocs, d)
		}
	}

	if deletedCount > 0 {
		newData, err := json.MarshalIndent(validDocs, "", "  ")
		if err != nil {
			return 0, err
		}
		if err := os.WriteFile(filePath, newData, 0644); err != nil {
			return 0, err
		}
	}

	return deletedCount, nil
}

func CloseDB() {
	if Pool != nil {
		Pool.Close()
	}
}

// Local Storage for Raw HTML Gzip Snapshots

type LocalStorage struct {
	baseDir string
}

func NewLocalStorage(baseDir string) *LocalStorage {
	return &LocalStorage{baseDir: baseDir}
}

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

	file, err := os.Create(filePath)
	if err != nil {
		return "", err
	}

	gw := gzip.NewWriter(file)

	_, copyErr := io.Copy(gw, bytes.NewReader(content))

	gwCloseErr := gw.Close()
	fileCloseErr := file.Close()

	if copyErr != nil || gwCloseErr != nil || fileCloseErr != nil {
		os.Remove(filePath)
		if copyErr != nil {
			return "", copyErr
		}
		if gwCloseErr != nil {
			return "", gwCloseErr
		}
		return "", fileCloseErr
	}

	return filePath, nil
}
