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

func TestGenerateFeatureVector(t *testing.T) {
	t.Run("Semantically Related Texts High Similarity", func(t *testing.T) {
		text1 := "Go concurrency goroutines"
		text2 := "Go parallel execution goroutines"

		vec1 := GenerateFeatureVector(text1, 128)
		vec2 := GenerateFeatureVector(text2, 128)

		sim := CosineSimilarity(vec1, vec2)
		if sim <= 0.50 {
			t.Errorf("expected high Cosine Similarity (> 0.50) for semantically related texts, got %.4f", sim)
		}
	})

	t.Run("Unrelated Texts Low Similarity", func(t *testing.T) {
		text1 := "Go concurrency"
		text2 := "baking a chocolate cake recipe"

		vec1 := GenerateFeatureVector(text1, 128)
		vec2 := GenerateFeatureVector(text2, 128)

		sim := CosineSimilarity(vec1, vec2)
		if sim >= 0.20 {
			t.Errorf("expected low Cosine Similarity (< 0.20) for unrelated texts, got %.4f", sim)
		}
	})
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
