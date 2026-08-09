package index

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/crawler-monorepo/internal/storage"
)

func TestEngineIndexAndSearch(t *testing.T) {
	eng := NewEngine()

	termPositions := map[string][]int{
		"golang": {0},
		"engine": {1},
		"search": {2},
	}

	eng.IndexDocument("https://golang.org", "Go Language", "Golang engine for search indexing", termPositions, 5)

	hits := eng.SearchBM25("golang search", 5)
	if len(hits) == 0 {
		t.Fatalf("Expected BM25 search hits, got 0")
	}

	if hits[0].DocID != "https://golang.org" {
		t.Errorf("Expected doc ID 'https://golang.org', got '%s'", hits[0].DocID)
	}

	eng.IndexDocumentVector("https://golang.org", "Go Language", "Golang engine for search indexing")
	vecHits := eng.SearchVector("golang search", 5)
	if len(vecHits) == 0 {
		t.Fatalf("Expected Vector search hits, got 0")
	}

	if vecHits[0].DocID != "https://golang.org" {
		t.Errorf("Expected doc ID 'https://golang.org', got '%s'", vecHits[0].DocID)
	}
}

func TestTrieAutocomplete(t *testing.T) {
	trie := NewTrie()
	trie.Insert("golang", 10)
	trie.Insert("goroutine", 5)
	trie.Insert("google", 2)

	results := trie.SearchPrefix("go", 5)
	if len(results) != 3 {
		t.Fatalf("Expected 3 autocomplete results, got %d", len(results))
	}

	if results[0].Term != "golang" {
		t.Errorf("Expected top result 'golang', got '%s'", results[0].Term)
	}
}

func TestSnapshotSaveLoad(t *testing.T) {
	tmpDir := t.TempDir()

	eng := NewEngine()
	termPositions := map[string][]int{"test": {0}}
	eng.IndexDocument("https://test.com", "Test Title", "Test Body", termPositions, 2)
	eng.IndexDocumentVector("https://test.com", "Test Title", "Test Body")

	err := eng.SaveSnapshot(tmpDir)
	if err != nil {
		t.Fatalf("SaveSnapshot failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(tmpDir, "inverted_index.json")); err != nil {
		t.Errorf("Expected inverted_index.json to exist")
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "vector_index.json")); err != nil {
		t.Errorf("Expected vector_index.json to exist")
	}

	eng2 := NewEngine()
	err = eng2.LoadSnapshot(tmpDir)
	if err != nil {
		t.Fatalf("LoadSnapshot failed: %v", err)
	}

	pl, exists := eng2.Inverted.GetPostingList("test")
	if !exists || len(pl.Entries) == 0 {
		t.Errorf("Expected inverted index to contain 'test' entry after restore")
	}
}

func TestCosineSimilarity(t *testing.T) {
	u := []float64{1.0, 0.0, 0.0}
	v := []float64{1.0, 0.0, 0.0}
	sim := CosineSimilarity(u, v)
	if mathAbs(sim-1.0) > 1e-5 {
		t.Errorf("Expected similarity 1.0 for identical vectors, got %f", sim)
	}
}

func mathAbs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func TestIndexDocumentIncrementalByURL(t *testing.T) {
	eng := NewEngine()
	termPositions1 := map[string][]int{"golang": {0}}
	eng.IndexDocument("https://golang.org", "Go Language", "Golang initial doc", termPositions1, 3)

	ctx := context.Background()
	doc2URL := "https://example.com/inc"
	_ = storage.SaveCrawledDocument(ctx, doc2URL, "Incremental Document", "Golang concurrency incremental text", 4, "web_crawled", doc2URL)

	err := eng.IndexDocumentIncrementalByURL(ctx, doc2URL)
	if err != nil {
		t.Fatalf("IndexDocumentIncrementalByURL failed: %v", err)
	}

	hits := eng.SearchBM25("golang", 10)
	if len(hits) < 2 {
		t.Fatalf("Expected at least 2 BM25 hits after incremental index, got %d", len(hits))
	}

	foundDoc1 := false
	foundDoc2 := false
	for _, h := range hits {
		if h.DocID == "https://golang.org" {
			foundDoc1 = true
		}
		if h.DocID == doc2URL {
			foundDoc2 = true
		}
	}

	if !foundDoc1 || !foundDoc2 {
		t.Errorf("Expected both doc1 and doc2 in index after incremental add, got doc1=%v, doc2=%v", foundDoc1, foundDoc2)
	}
}
