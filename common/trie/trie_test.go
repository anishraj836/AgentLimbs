package trie

import (
	"testing"
)

func TestTrieAutocomplete(t *testing.T) {
	tr := NewTrie()

	tr.Insert("project", 10)
	tr.Insert("program", 15)
	tr.Insert("progress", 5)
	tr.Insert("production", 20)
	tr.Insert("prototype", 8)
	tr.Insert("python", 25)

	// Search prefix 'pro'
	results := tr.SearchPrefix("pro", 3)
	if len(results) != 3 {
		t.Fatalf("expected 3 results for prefix 'pro', got %d", len(results))
	}

	// Order by frequency desc: production (20), program (15), project (10)
	expectedOrder := []string{"production", "program", "project"}
	for i, r := range results {
		if r.Term != expectedOrder[i] {
			t.Errorf("at index %d: expected %q, got %q", i, expectedOrder[i], r.Term)
		}
	}
}
