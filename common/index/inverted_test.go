package index

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInvertedIndexSnapshot(t *testing.T) {
	idx := NewInvertedIndex()
	idx.AddDocument("doc1", map[string][]int{
		"golang": {0},
		"search": {1},
	}, 2)

	tmpDir, err := os.MkdirTemp("", "idx_snap_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	snapPath := filepath.Join(tmpDir, "inverted.json")
	if err := idx.SaveSnapshot(snapPath); err != nil {
		t.Fatalf("SaveSnapshot failed: %v", err)
	}

	restoredIdx := NewInvertedIndex()
	if err := restoredIdx.LoadSnapshot(snapPath); err != nil {
		t.Fatalf("LoadSnapshot failed: %v", err)
	}

	pl, exists := restoredIdx.GetPostingList("golang")
	if !exists || len(pl.Entries) != 1 || pl.Entries[0].DocID != "doc1" {
		t.Errorf("failed to restore posting list correctly: %v", pl)
	}
}
