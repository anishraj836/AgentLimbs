package storage

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestEdgeCases_Concurrency(t *testing.T) {
	ctx := context.Background()

	runConcurrencyTest := func(t *testing.T, store DocumentStore) {
		const numGoroutines = 50
		var wg sync.WaitGroup

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()

				// Write
				doc := &CrawledDocument{
					URL:       "https://example.com/doc",
					Title:     "Title",
					CleanBody: "Body",
				}
				_ = store.Save(ctx, doc, time.Millisecond*10)

				// Read
				_, _ = store.GetByURL(ctx, "https://example.com/doc")

				// List
				_, _ = store.List(ctx, 10, 0)

				// DeleteExpired
				_, _ = store.DeleteExpired(ctx)
			}(i)
		}
		wg.Wait()
	}

	t.Run("FileStore", func(t *testing.T) {
		tmpDir := t.TempDir()
		store := NewFileStore(filepath.Join(tmpDir, "test.json"))
		runConcurrencyTest(t, store)
	})

	t.Run("MemoryStore", func(t *testing.T) {
		store := NewMemoryStore()
		runConcurrencyTest(t, store)
	})

	t.Run("RepositoryFacade", func(t *testing.T) {
		tmpDir := t.TempDir()
		InitDB("") // file fallback
		SetGlobalStore(NewFileStore(filepath.Join(tmpDir, "test2.json")))
		
		const numGoroutines = 50
		var wg sync.WaitGroup
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = SaveCrawledDocumentWithTTL(ctx, "https://example.com/doc", "Title", "Body", 10, "web", "", time.Millisecond*10)
				_, _ = GetCrawledDocumentByURL(ctx, "https://example.com/doc")
				_, _ = GetCrawledDocuments(ctx)
				_, _ = DeleteExpiredDocuments(ctx)
			}()
		}
		wg.Wait()
	})
}

func TestEdgeCases_MalformedJSONRecovery(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "corrupted.json")
	
	// Write corrupted garbage
	_ = os.WriteFile(filePath, []byte("{invalid_json: true,"), 0644)

	store := NewFileStore(filePath)
	ctx := context.Background()
	
	// Should gracefully handle list
	_, err := store.List(ctx, 10, 0)
	if err != nil {
		t.Errorf("Expected List to recover or handle malformed JSON, got err: %v", err)
	}
	
	_, err = store.GetByURL(ctx, "abc")
	if err != nil {
		t.Errorf("Expected GetByURL to recover or handle malformed JSON, got err: %v", err)
	}

	doc := &CrawledDocument{URL: "https://example.com"}
	err = store.Save(ctx, doc, 0)
	if err != nil {
		t.Errorf("Expected Save to recover and overwrite malformed JSON, got err: %v", err)
	}
	
	// Ensure file is now valid
	docs, err := store.List(ctx, 10, 0)
	if err != nil || len(docs) == 0 {
		t.Errorf("Expected valid docs after Save, got %v, err: %v", docs, err)
	}
}

func TestEdgeCases_ZeroByteFilesEmptyPtrsEmptyURLs(t *testing.T) {
	ctx := context.Background()

	t.Run("ZeroByteFile", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "empty.json")
		_ = os.WriteFile(filePath, []byte(""), 0644)

		store := NewFileStore(filePath)
		docs, err := store.List(ctx, 10, 0)
		if err != nil {
			t.Errorf("Expected success on zero-byte file, got err: %v", err)
		}
		if len(docs) != 0 {
			t.Errorf("Expected 0 docs, got %d", len(docs))
		}
	})

	t.Run("EmptyURL", func(t *testing.T) {
		store := NewMemoryStore()
		
		doc := &CrawledDocument{URL: ""}
		err := store.Save(ctx, doc, 0)
		if err == nil {
			t.Errorf("Expected error when saving empty URL")
		}

		res, err := store.GetByURL(ctx, "")
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
		if res != nil {
			t.Errorf("Expected nil response for empty URL")
		}
	})

	t.Run("NilDocPointer", func(t *testing.T) {
		store := NewMemoryStore()
		err := store.Save(ctx, nil, 0)
		if err == nil {
			t.Errorf("Expected error when saving nil document")
		}
	})
}

func TestEdgeCases_ActiveTTLExpiration(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	t.Run("NegativeTTL", func(t *testing.T) {
		doc := &CrawledDocument{URL: "https://example.com/neg"}
		_ = store.Save(ctx, doc, -time.Hour)

		res, _ := store.GetByURL(ctx, "https://example.com/neg")
		if res != nil {
			t.Errorf("Expected nil for expired negative TTL doc")
		}
	})

	t.Run("ZeroTTL", func(t *testing.T) {
		doc := &CrawledDocument{URL: "https://example.com/zero"}
		_ = store.Save(ctx, doc, 0)

		res, _ := store.GetByURL(ctx, "https://example.com/zero")
		if res == nil {
			t.Errorf("Expected doc to persist with 0 TTL")
		}
	})

	t.Run("SubMillisecondTTL", func(t *testing.T) {
		doc := &CrawledDocument{URL: "https://example.com/subms"}
		_ = store.Save(ctx, doc, time.Microsecond)
		
		time.Sleep(time.Millisecond * 2) // Ensure it expires
		res, _ := store.GetByURL(ctx, "https://example.com/subms")
		if res != nil {
			t.Errorf("Expected nil for expired sub-ms TTL doc")
		}
	})
}

func TestEdgeCases_CircularMultiHopAlias(t *testing.T) {
	ctx := context.Background()

	t.Run("MemoryStore", func(t *testing.T) {
		store := NewMemoryStore()
		_ = store.SaveAlias(ctx, "a", "b")
		_ = store.SaveAlias(ctx, "b", "c")
		_ = store.SaveAlias(ctx, "c", "d")

		res := store.GetAlias(ctx, "a")
		if res != "d" {
			t.Errorf("Expected multi-hop alias 'a' -> 'd', got %s", res)
		}

		// Circular
		_ = store.SaveAlias(ctx, "x", "y")
		_ = store.SaveAlias(ctx, "y", "z")
		_ = store.SaveAlias(ctx, "z", "x")

		res2 := store.GetAlias(ctx, "x")
		if res2 == "" {
			t.Errorf("Expected some fallback for circular alias, but should not infinite loop")
		}
	})

	t.Run("FileStore", func(t *testing.T) {
		store := NewFileStore(filepath.Join(t.TempDir(), "test.json"))
		_ = store.SaveAlias(ctx, "a", "b")
		_ = store.SaveAlias(ctx, "b", "c")
		_ = store.SaveAlias(ctx, "c", "d")

		res := store.GetAlias(ctx, "a")
		if res != "d" {
			t.Errorf("Expected multi-hop alias 'a' -> 'd', got %s", res)
		}
	})
}

func TestEdgeCases_ExtremePagination(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	_ = store.Save(ctx, &CrawledDocument{URL: "u1"}, 0)
	_ = store.Save(ctx, &CrawledDocument{URL: "u2"}, 0)
	_ = store.Save(ctx, &CrawledDocument{URL: "u3"}, 0)

	tests := []struct {
		limit  int
		offset int
		expect int
	}{
		{-1, 0, 3},
		{0, 0, 3},
		{1000000, 0, 3},
		{2, -5, 2}, // negative offset should be treated as 0
		{10, 100, 0},
		{-1, 100, 0},
	}

	for _, tc := range tests {
		res, _ := store.List(ctx, tc.limit, tc.offset)
		if len(res) != tc.expect {
			t.Errorf("For limit=%d offset=%d, expected %d docs, got %d", tc.limit, tc.offset, tc.expect, len(res))
		}
	}
}

func TestEdgeCases_HotSwappingGlobalStore(t *testing.T) {
	ctx := context.Background()

	InitDB("") // sets global store

	const numGoroutines = 20
	var wg sync.WaitGroup

	// Start 20 goroutines querying and saving
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = SaveCrawledDocument(ctx, "hot-swap", "title", "body", 10, "web", "")
				_, _ = GetCrawledDocumentByURL(ctx, "hot-swap")
			}
		}(i)
	}

	// Hot swap while running
	for i := 0; i < 10; i++ {
		if i%2 == 0 {
			SetGlobalStore(NewMemoryStore())
		} else {
			SetGlobalStore(NewFileStore(filepath.Join(t.TempDir(), "hot-swap.json")))
		}
		time.Sleep(time.Millisecond * 2)
	}

	wg.Wait()
}

func TestEdgeCases_LocalStorageConcurrentWrites(t *testing.T) {
	tmpDir := t.TempDir()
	localStore := NewLocalStorage(tmpDir)

	const numGoroutines = 50
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_, err := localStore.Save("https://example.com/same", []byte("HTML CONTENT"))
			if err != nil {
				t.Errorf("Unexpected error on concurrent local save: %v", err)
			}
		}(i)
	}
	wg.Wait()
}
