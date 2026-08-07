package db

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestDocumentTTLAndJanitor(t *testing.T) {
	// Clean up storage file before and after test execution
	defer os.Remove(getStoragePath())
	_ = os.Remove(getStoragePath())

	ctx := context.Background()

	// 1. Insert a document with 100ms TTL
	err := SaveCrawledDocumentWithTTL(
		ctx,
		"https://example.com/expiring",
		"Expiring Doc",
		"This document will expire soon",
		5,
		"web_crawled",
		"https://example.com/expiring",
		100*time.Millisecond,
	)
	if err != nil {
		t.Fatalf("failed to save document with TTL: %v", err)
	}

	// Verify document exists before expiration
	docs, err := GetCrawledDocuments(ctx)
	if err != nil {
		t.Fatalf("failed to get crawled documents: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 document before expiry, got %d", len(docs))
	}

	// 2. Sleep 150ms to allow document to expire
	time.Sleep(150 * time.Millisecond)

	// 3. Run DeleteExpiredDocuments
	deleted, err := DeleteExpiredDocuments(ctx)
	if err != nil {
		t.Fatalf("failed to delete expired documents: %v", err)
	}
	if deleted < 1 {
		t.Fatalf("expected at least 1 document deleted by janitor, got %d", deleted)
	}

	// 4. Verify GetCrawledDocuments returns 0 docs
	docsAfter, err := GetCrawledDocuments(ctx)
	if err != nil {
		t.Fatalf("failed to get crawled documents after deletion: %v", err)
	}
	if len(docsAfter) != 0 {
		t.Fatalf("expected 0 documents after expiry and cleanup, got %d", len(docsAfter))
	}
}
