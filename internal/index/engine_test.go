package index

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

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

	// Verify temporary files are cleaned up
	if _, err := os.Stat(filepath.Join(tmpDir, "inverted_index.json.tmp")); !os.IsNotExist(err) {
		t.Errorf("Expected inverted_index.json.tmp to not exist after successful save")
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "vector_index.json.tmp")); !os.IsNotExist(err) {
		t.Errorf("Expected vector_index.json.tmp to not exist after successful save")
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

func TestURLAliasDeduplication(t *testing.T) {
	eng := NewEngine()
	targetURL := "http://example.com"
	canonicalURL := "https://example.com/canonical"

	eng.IndexDocumentDirectly(canonicalURL, "Canonical Page", "Content of canonical page with unique text", 6, targetURL)

	// Verify metadata lookup by canonical URL
	title1, _, _, exists1 := eng.GetDocumentMetadata(canonicalURL)
	if !exists1 || title1 != "Canonical Page" {
		t.Errorf("Expected canonical URL lookup to succeed, got title '%s', exists=%v", title1, exists1)
	}

	// Verify metadata lookup by target/alias URL resolves to canonical document
	title2, _, _, exists2 := eng.GetDocumentMetadata(targetURL)
	if !exists2 || title2 != "Canonical Page" {
		t.Errorf("Expected alias URL lookup to resolve to canonical document, got title '%s', exists=%v", title2, exists2)
	}

	// Verify no duplicate entries in metadata maps
	titles, _, _ := eng.GetMetadataMaps()
	if len(titles) != 1 {
		t.Errorf("Expected exactly 1 metadata entry (deduplicated), got %d entries", len(titles))
	}
}

func TestPostingListDeepCopyConcurrency(t *testing.T) {
	inv := NewInvertedIndex()
	inv.AddDocument("doc1", map[string][]int{"go": {1, 5, 10}}, 15)

	done := make(chan bool)
	go func() {
		for i := 0; i < 100; i++ {
			inv.AddDocument("doc1", map[string][]int{"go": {i, i + 1}}, 20)
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			if pl, exists := inv.GetPostingList("go"); exists && len(pl.Entries) > 0 {
				// Mutate returned positions slice locally to test isolation
				if len(pl.Entries[0].Positions) > 0 {
					pl.Entries[0].Positions[0] = 99999
				}
			}
		}
		done <- true
	}()

	<-done
	<-done
}

func TestVectorIndexSaveSnapshotDeepCopyConcurrency(t *testing.T) {
	tmpDir := t.TempDir()
	vi := NewVectorIndex(3)
	_ = vi.AddVector("doc1", []float64{0.1, 0.2, 0.3})

	done := make(chan bool)
	go func() {
		for i := 0; i < 50; i++ {
			_ = vi.AddVector(filepath.Join("doc", string(rune('a'+i%26))), []float64{float64(i), 0.5, 0.9})
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 20; i++ {
			snapPath := filepath.Join(tmpDir, "vec_snap.json")
			_ = vi.SaveSnapshot(snapPath)
		}
		done <- true
	}()

	<-done
	<-done
}

func TestEmpiricalSemanticSimilarityAndScanScaling(t *testing.T) {
	// 1. Measure exact cosine similarity between semantic synonyms vs morphological variants
	vCar := GenerateFeatureVector("car", 128)
	vAutomobile := GenerateFeatureVector("automobile", 128)
	vVehicle := GenerateFeatureVector("vehicle", 128)
	vSportsCar := GenerateFeatureVector("sports car", 128)
	vCars := GenerateFeatureVector("cars", 128)

	simCarAuto := CosineSimilarity(vCar, vAutomobile)
	simCarVehicle := CosineSimilarity(vCar, vVehicle)
	simCarSports := CosineSimilarity(vCar, vSportsCar)
	simCarCars := CosineSimilarity(vCar, vCars)

	t.Logf("Empirical Cosine Similarity:")
	t.Logf("  'car' vs 'automobile': %.4f", simCarAuto)
	t.Logf("  'car' vs 'vehicle':    %.4f", simCarVehicle)
	t.Logf("  'car' vs 'sports car': %.4f", simCarSports)
	t.Logf("  'car' vs 'cars':       %.4f", simCarCars)

	// 2. Measure flat O(N) vector scan latency across 1k, 10k, and 50k vectors
	sizes := []int{1000, 10000, 50000}
	for _, size := range sizes {
		vi := NewVectorIndex(128)
		qVec := GenerateFeatureVector("search query test", 128)
		for i := 0; i < size; i++ {
			_ = vi.AddVector(string(rune(i)), GenerateFeatureVector(string(rune(i*37)), 128))
		}

		// Warm up and measure
		start := time.Now()
		runs := 50
		for r := 0; r < runs; r++ {
			_ = vi.SearchNearest(qVec, 10)
		}
		avgDuration := time.Since(start) / time.Duration(runs)
		t.Logf("Flat O(N) scan latency for N=%d vectors: %v/query", size, avgDuration)
	}
}

func TestVectorIndex_DeleteVector(t *testing.T) {
	vi := NewVectorIndex(3)
	vec := []float64{1.0, 0.0, 0.0}
	_ = vi.AddVector("doc1", vec)

	res := vi.SearchNearest(vec, 5)
	if len(res) == 0 || res[0].DocID != "doc1" {
		t.Fatalf("Expected doc1 in nearest results before deletion")
	}

	vi.DeleteVector("doc1")

	resAfter := vi.SearchNearest(vec, 5)
	if len(resAfter) != 0 {
		t.Errorf("Expected 0 results after deleting doc1, got %d", len(resAfter))
	}
}
