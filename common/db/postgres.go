package db

import (
	"context"
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
	if Pool == nil {
		return nil
	}
	if sourceType == "" {
		sourceType = "web_crawled"
	}
	if sourceURL == "" {
		sourceURL = url
	}
	query := `
	INSERT INTO crawled_pages (url, title, clean_body, total_tokens, source_type, source_url)
	VALUES ($1, $2, $3, $4, $5, $6)
	ON CONFLICT (url) DO UPDATE
	SET title = EXCLUDED.title, clean_body = EXCLUDED.clean_body, total_tokens = EXCLUDED.total_tokens, source_type = EXCLUDED.source_type, source_url = EXCLUDED.source_url;`
	_, err := Pool.Exec(ctx, query, url, title, cleanBody, totalTokens, sourceType, sourceURL)
	return err
}

func GetCrawledDocuments(ctx context.Context) ([]CrawledDocument, error) {
	if Pool == nil {
		return nil, nil
	}
	query := `SELECT id, url, title, clean_body, total_tokens, COALESCE(source_type, 'web_crawled'), COALESCE(source_url, url) FROM crawled_pages ORDER BY id ASC;`
	rows, err := Pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var docs []CrawledDocument
	for rows.Next() {
		var doc CrawledDocument
		if err := rows.Scan(&doc.ID, &doc.URL, &doc.Title, &doc.CleanBody, &doc.TotalTokens, &doc.SourceType, &doc.SourceURL); err == nil {
			docs = append(docs, doc)
		}
	}
	return docs, nil
}

func CloseDB() {
	if Pool != nil {
		Pool.Close()
	}
}
