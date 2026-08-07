package index

import (
	"os"
	"path/filepath"
	"testing"
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
