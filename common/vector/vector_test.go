package vector

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestCosineSimilarity(t *testing.T) {
	v1 := []float64{1.0, 0.0, 0.0}
	v2 := []float64{1.0, 0.0, 0.0}
	v3 := []float64{0.0, 1.0, 0.0}

	simIdentical := CosineSimilarity(v1, v2)
	if math.Abs(simIdentical-1.0) > 0.0001 {
		t.Errorf("expected CosineSimilarity of identical vectors to be 1.0, got %f", simIdentical)
	}

	simOrthogonal := CosineSimilarity(v1, v3)
	if math.Abs(simOrthogonal-0.0) > 0.0001 {
		t.Errorf("expected CosineSimilarity of orthogonal vectors to be 0.0, got %f", simOrthogonal)
	}
}

func TestVectorIndex(t *testing.T) {
	idx := NewVectorIndex(3)

	doc1 := []float64{0.8, 0.6, 0.0}
	doc2 := []float64{0.0, 0.0, 1.0}

	idx.AddVector("doc1", doc1)
	idx.AddVector("doc2", doc2)

	query := []float64{0.9, 0.4, 0.0}
	results := idx.SearchNearest(query, 2)

	if len(results) == 0 {
		t.Fatalf("expected vector search results, got none")
	}

	if results[0].DocID != "doc1" {
		t.Errorf("expected doc1 to rank nearest to query, got %s", results[0].DocID)
	}
}

func TestVectorSnapshot(t *testing.T) {
	idx := NewVectorIndex(3)
	doc1 := []float64{0.8, 0.6, 0.0}
	idx.AddVector("doc1", doc1)

	tmpDir, err := os.MkdirTemp("", "vec_snap_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	snapPath := filepath.Join(tmpDir, "vector.json")
	if err := idx.SaveSnapshot(snapPath); err != nil {
		t.Fatalf("SaveSnapshot failed: %v", err)
	}

	restoredIdx := NewVectorIndex(3)
	if err := restoredIdx.LoadSnapshot(snapPath); err != nil {
		t.Fatalf("LoadSnapshot failed: %v", err)
	}

	results := restoredIdx.SearchNearest([]float64{0.8, 0.6, 0.0}, 1)
	if len(results) == 0 || results[0].DocID != "doc1" {
		t.Errorf("failed to restore vector index correctly: %v", results)
	}
}
