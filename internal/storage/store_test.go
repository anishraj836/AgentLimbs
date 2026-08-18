package storage

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestDocumentStore_MemoryStore(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	if store.DriverName() != "memory" {
		t.Errorf("expected driver name 'memory', got %q", store.DriverName())
	}

	doc := &CrawledDocument{
		URL:         "https://go.dev/doc/effective_go",
		Title:       "Effective Go",
		CleanBody:   "Tips for writing clear, idiomatic Go code.",
		TotalTokens: 120,
		SourceType:  "web_crawled",
		SourceURL:   "https://go.dev/doc/effective_go",
	}

	if err := store.Save(ctx, doc, 0); err != nil {
		t.Fatalf("MemoryStore Save failed: %v", err)
	}

	retrieved, err := store.GetByURL(ctx, "https://go.dev/doc/effective_go")
	if err != nil {
		t.Fatalf("MemoryStore GetByURL failed: %v", err)
	}
	if retrieved == nil || retrieved.Title != "Effective Go" {
		t.Errorf("expected title 'Effective Go', got %+v", retrieved)
	}

	// Test List with pagination
	list, err := store.List(ctx, 10, 0)
	if err != nil {
		t.Fatalf("MemoryStore List failed: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 document in list, got %d", len(list))
	}

	// Test URL Alias
	if err := store.SaveAlias(ctx, "https://golang.org/doc/effective_go", "https://go.dev/doc/effective_go"); err != nil {
		t.Fatalf("SaveAlias failed: %v", err)
	}
	aliasedDoc, err := store.GetByURL(ctx, "https://golang.org/doc/effective_go")
	if err != nil {
		t.Fatalf("GetByURL with alias failed: %v", err)
	}
	if aliasedDoc == nil || aliasedDoc.Title != "Effective Go" {
		t.Errorf("expected aliased doc to resolve to 'Effective Go', got %+v", aliasedDoc)
	}

	// Test TTL Expiration
	expiringDoc := &CrawledDocument{
		URL:         "https://example.com/ephemeral",
		Title:       "Ephemeral",
		CleanBody:   "Vanishing soon",
		TotalTokens: 10,
	}
	if err := store.Save(ctx, expiringDoc, 10*time.Millisecond); err != nil {
		t.Fatalf("Save with TTL failed: %v", err)
	}

	time.Sleep(30 * time.Millisecond)
	deleted, err := store.DeleteExpired(ctx)
	if err != nil {
		t.Fatalf("DeleteExpired failed: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 deleted expired document, got %d", deleted)
	}

	// Test Ping & Close
	if err := store.Ping(ctx); err != nil {
		t.Errorf("Ping failed: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
}

func TestDocumentStore_FileStore(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	store := NewFileStore(filepath.Join(tmpDir, "docs.json"))

	if store.DriverName() != "file" {
		t.Errorf("expected driver name 'file', got %q", store.DriverName())
	}

	doc1 := &CrawledDocument{
		URL:         "https://docker.com/engine",
		Title:       "Docker Engine",
		CleanBody:   "Container runtime daemon",
		TotalTokens: 50,
	}

	if err := store.Save(ctx, doc1, 0); err != nil {
		t.Fatalf("FileStore Save failed: %v", err)
	}

	res, err := store.GetByURL(ctx, "https://docker.com/engine")
	if err != nil {
		t.Fatalf("FileStore GetByURL failed: %v", err)
	}
	if res == nil || res.Title != "Docker Engine" {
		t.Errorf("expected title 'Docker Engine', got %+v", res)
	}

	// Test concurrent writes
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			d := &CrawledDocument{
				URL:         filepath.Join("https://concurrent.com", string(rune('a'+idx))),
				Title:       "Concurrent",
				CleanBody:   "Payload",
				TotalTokens: 5,
			}
			_ = store.Save(ctx, d, 0)
		}(i)
	}
	wg.Wait()

	allDocs, err := store.List(ctx, 100, 0)
	if err != nil {
		t.Fatalf("FileStore List failed: %v", err)
	}
	if len(allDocs) != 11 {
		t.Errorf("expected 11 documents after concurrent saves, got %d", len(allDocs))
	}
}

func TestDocumentStore_Factory(t *testing.T) {
	memStore, err := NewStore("memory", "")
	if err != nil || memStore.DriverName() != "memory" {
		t.Fatalf("NewStore memory failed: %v", err)
	}

	tmpDir := t.TempDir()
	fileStore, err := NewStore("file", filepath.Join(tmpDir, "custom.json"))
	if err != nil || fileStore.DriverName() != "file" {
		t.Fatalf("NewStore file failed: %v", err)
	}

	_, err = NewStore("invalid_driver", "")
	if err == nil {
		t.Errorf("expected error for invalid driver, got nil")
	}

	// Test Global Store
	SetGlobalStore(memStore)
	if GetGlobalStore().DriverName() != "memory" {
		t.Errorf("expected global store to be memory, got %q", GetGlobalStore().DriverName())
	}
}
