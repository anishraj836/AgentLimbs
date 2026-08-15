package index

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVectorIndex_MultiPrecisionAndMigration(t *testing.T) {
	dim := 4

	// 1. Float32 index insertion and search
	vi := NewVectorIndexWithPrecision(dim, PrecisionFloat32)
	if vi.GetPrecision() != PrecisionFloat32 {
		t.Fatalf("expected precision float32, got %s", vi.GetPrecision())
	}

	doc1 := []float64{1.0, 0.0, 0.0, 0.0}
	doc2 := []float64{0.0, 1.0, 0.0, 0.0}
	doc3 := []float64{0.707, 0.707, 0.0, 0.0}

	_ = vi.AddVector("doc1", doc1)
	_ = vi.AddVector("doc2", doc2)
	_ = vi.AddVector("doc3", doc3)

	query := []float64{1.0, 0.0, 0.0, 0.0}
	res := vi.SearchNearest(query, 3)
	if len(res) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(res))
	}
	if res[0].DocID != "doc1" {
		t.Errorf("expected doc1 as top result, got %s", res[0].DocID)
	}

	// 2. Runtime SetPrecision to Int8
	if err := vi.SetPrecision(PrecisionInt8); err != nil {
		t.Fatalf("SetPrecision to Int8 failed: %v", err)
	}
	if vi.GetPrecision() != PrecisionInt8 {
		t.Fatalf("expected precision int8, got %s", vi.GetPrecision())
	}

	// Verify search results still match in Int8
	resInt8 := vi.SearchNearest(query, 3)
	if len(resInt8) < 2 || resInt8[0].DocID != "doc1" {
		t.Fatalf("unexpected Int8 search results: %v", resInt8)
	}

	// 3. Save snapshot in Int8 and reload into Float32 index
	tmpDir := t.TempDir()
	snapFile := filepath.Join(tmpDir, "vectors_test.json")

	if err := vi.SaveSnapshot(snapFile); err != nil {
		t.Fatalf("SaveSnapshot failed: %v", err)
	}

	viReload := NewVectorIndexWithPrecision(dim, PrecisionFloat32)
	if err := viReload.LoadSnapshot(snapFile); err != nil {
		t.Fatalf("LoadSnapshot failed: %v", err)
	}

	resReload := viReload.SearchNearest(query, 3)
	if len(resReload) < 2 || resReload[0].DocID != "doc1" {
		t.Fatalf("unexpected reload search results: %v", resReload)
	}
}

func TestVectorIndex_LegacyFloat64SnapshotMigration(t *testing.T) {
	dim := 3
	tmpDir := t.TempDir()
	snapFile := filepath.Join(tmpDir, "legacy_vectors.json")

	legacyJSON := `{
		"dimensions": 3,
		"vectors": {
			"docA": [1.0, 0.0, 0.0],
			"docB": [0.0, 1.0, 0.0]
		}
	}`
	if err := os.WriteFile(snapFile, []byte(legacyJSON), 0644); err != nil {
		t.Fatalf("failed to write legacy snapshot: %v", err)
	}

	// Load legacy snapshot into Int8 index
	vi := NewVectorIndexWithPrecision(dim, PrecisionInt8)
	if err := vi.LoadSnapshot(snapFile); err != nil {
		t.Fatalf("LoadSnapshot of legacy float64 failed: %v", err)
	}

	res := vi.SearchNearest([]float64{1.0, 0.0, 0.0}, 2)
	if len(res) == 0 || res[0].DocID != "docA" {
		t.Fatalf("expected docA, got %v", res)
	}
}
