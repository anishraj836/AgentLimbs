package db

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
)

var Pool *pgxpool.Pool

type CrawledDocument struct {
	ID          int    `json:"id"`
	URL         string `json:"url"`
	Title       string `json:"title"`
	CleanBody   string `json:"clean_body"`
	TotalTokens int    `json:"total_tokens"`
	SourceType  string `json:"source_type"`
	SourceURL   string `json:"source_url"`
}

func InitDB(databaseURL string) {
	if databaseURL == "" {
		databaseURL = "postgres://crawler:crawler_password@localhost:5432/crawler_db"
	}
	var err error
	Pool, err = pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		return // Silent fallback to in-memory if Postgres is unavailable
	}
	InitTables(context.Background())
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
		created_at TIMESTAMPTZ DEFAULT NOW()
	);`
	_, err := Pool.Exec(ctx, query)
	return err
}

func SaveCrawledDocument(ctx context.Context, url, title, cleanBody string, totalTokens int, sourceType, sourceURL string) error {
	if sourceType == "" {
		sourceType = "web_crawled"
	}
	if sourceURL == "" {
		sourceURL = url
	}

	if Pool != nil {
		query := `
		INSERT INTO crawled_pages (url, title, clean_body, total_tokens, source_type, source_url)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (url) DO UPDATE
		SET title = EXCLUDED.title, clean_body = EXCLUDED.clean_body, total_tokens = EXCLUDED.total_tokens, source_type = EXCLUDED.source_type, source_url = EXCLUDED.source_url;`
		_, err := Pool.Exec(ctx, query, url, title, cleanBody, totalTokens, sourceType, sourceURL)
		if err == nil {
			return nil
		}
	}

	// Fallback to shared file storage
	return saveToFileFallback(url, title, cleanBody, totalTokens, sourceType, sourceURL)
}

func GetCrawledDocuments(ctx context.Context) ([]CrawledDocument, error) {
	if Pool != nil {
		query := `SELECT id, url, title, clean_body, total_tokens, COALESCE(source_type, 'web_crawled'), COALESCE(source_url, url) FROM crawled_pages ORDER BY id ASC;`
		rows, err := Pool.Query(ctx, query)
		if err == nil {
			defer rows.Close()
			var docs []CrawledDocument
			for rows.Next() {
				var doc CrawledDocument
				if err := rows.Scan(&doc.ID, &doc.URL, &doc.Title, &doc.CleanBody, &doc.TotalTokens, &doc.SourceType, &doc.SourceURL); err == nil {
					docs = append(docs, doc)
				}
			}
			if len(docs) > 0 {
				return docs, nil
			}
		}
	}

	// Fallback to shared file storage
	return getFromFileFallback()
}

var fallbackMu sync.Mutex

func getStoragePath() string {
	dir := "data"
	_ = os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "crawled_pages.json")
}

func saveToFileFallback(url, title, cleanBody string, totalTokens int, sourceType, sourceURL string) error {
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

	return docs, nil
}

func CloseDB() {
	if Pool != nil {
		Pool.Close()
	}
}
