package storage

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestFileFallbackStorage(t *testing.T) {
	ctx := context.Background()

	err := SaveCrawledDocument(ctx, "https://example.com/doc1", "Doc 1", "Body text 1", 10, "test", "https://example.com/doc1")
	if err != nil {
		t.Fatalf("SaveCrawledDocument failed: %v", err)
	}

	docs, err := GetCrawledDocuments(ctx)
	if err != nil {
		t.Fatalf("GetCrawledDocuments failed: %v", err)
	}

	found := false
	for _, d := range docs {
		if d.URL == "https://example.com/doc1" {
			found = true
			if d.Title != "Doc 1" {
				t.Errorf("Expected title 'Doc 1', got '%s'", d.Title)
			}
			break
		}
	}
	if !found {
		t.Errorf("Saved document not found in GetCrawledDocuments")
	}

	// Test TTL expiration
	err = SaveCrawledDocumentWithTTL(ctx, "https://example.com/expiring", "Expiring Doc", "Body", 5, "test", "https://example.com/expiring", 10*time.Millisecond)
	if err != nil {
		t.Fatalf("SaveCrawledDocumentWithTTL failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	deleted, err := DeleteExpiredDocuments(ctx)
	if err != nil {
		t.Fatalf("DeleteExpiredDocuments failed: %v", err)
	}

	if deleted < 1 {
		t.Errorf("Expected at least 1 deleted document, got %d", deleted)
	}
}

func TestLocalStorageSave(t *testing.T) {
	tmpDir := t.TempDir()
	ls := NewLocalStorage(tmpDir)

	url := "https://golang.org/doc/effective_go"
	content := []byte("<html><body><h1>Effective Go</h1></body></html>")

	filePath, err := ls.Save(url, content)
	if err != nil {
		t.Fatalf("LocalStorage Save failed: %v", err)
	}

	if _, err := os.Stat(filePath); err != nil {
		t.Errorf("Expected saved gzip file at %s to exist", filePath)
	}
}
