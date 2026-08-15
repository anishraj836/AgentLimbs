package storage

import (
	"context"
	"os"
	"path/filepath"
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

	// Test TTL expiration with polling to prevent flakiness on slow runners
	err = SaveCrawledDocumentWithTTL(ctx, "https://example.com/expiring", "Expiring Doc", "Body", 5, "test", "https://example.com/expiring", 20*time.Millisecond)
	if err != nil {
		t.Fatalf("SaveCrawledDocumentWithTTL failed: %v", err)
	}

	var deleted int64
	for i := 0; i < 30; i++ {
		time.Sleep(25 * time.Millisecond)
		deleted, err = DeleteExpiredDocuments(ctx)
		if err != nil {
			t.Fatalf("DeleteExpiredDocuments failed: %v", err)
		}
		if deleted >= 1 {
			break
		}
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

func TestPingDB_FileFallback(t *testing.T) {
	ctx := context.Background()
	// When Pool is nil, PingDB returns nil (fallback mode)
	Pool = nil
	err := PingDB(ctx)
	if err != nil {
		t.Errorf("expected PingDB to return nil in fallback mode, got %v", err)
	}

	// Even if DATABASE_URL is set, if Pool is nil, return nil (local/fallback mode)
	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	defer os.Unsetenv("DATABASE_URL")
	err = PingDB(ctx)
	if err != nil {
		t.Errorf("expected PingDB to return nil when Pool == nil even with DATABASE_URL set, got %v", err)
	}
}

func TestInitDB_UnreachableConnectionFallback(t *testing.T) {
	// With an unreachable address, InitDB should gracefully complete retries and ensure Pool == nil
	Pool = nil
	InitDB("postgres://invalid_user:invalid_pass@127.0.0.1:54329/nonexistent_db?connect_timeout=1")
	if Pool != nil {
		t.Errorf("Expected Pool to be nil after unreachable InitDB, got non-nil pointer")
	}
}

func TestWriteDirectly(t *testing.T) {
	tmpDir := t.TempDir()
	dst := filepath.Join(tmpDir, "direct_test.json")
	data := []byte(`{"direct": true}`)

	if err := writeDirectly(dst, data, 0644); err != nil {
		t.Fatalf("writeDirectly failed: %v", err)
	}

	read, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("Failed to read directly written file: %v", err)
	}
	if string(read) != string(data) {
		t.Errorf("Expected file content %s, got %s", string(data), string(read))
	}
}
