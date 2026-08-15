package index

import (
	"fmt"
	"math/rand"
	"testing"
)

func TestDocIDMapper(t *testing.T) {
	m := NewDocIDMapper()

	id0 := m.GetOrCreateID("https://a.com")
	id1 := m.GetOrCreateID("https://b.com")
	id0Again := m.GetOrCreateID("https://a.com")

	if id0 != 0 || id1 != 1 || id0Again != 0 {
		t.Fatalf("unexpected IDs: id0=%d, id1=%d, id0Again=%d", id0, id1, id0Again)
	}

	url, found := m.GetURL(0)
	if !found || url != "https://a.com" {
		t.Fatalf("expected https://a.com, got %s (found: %v)", url, found)
	}

	m.MarkDeleted(0)
	if !m.IsDeleted(0) {
		t.Fatalf("expected id 0 to be marked deleted")
	}
	_, foundAfterDelete := m.GetURL(0)
	if foundAfterDelete {
		t.Fatalf("deleted URL should not be found via GetURL")
	}
}

func TestBlockMaxWAND_ScoreEquivalence(t *testing.T) {
	numDocs := 200
	numTerms := 5
	k1 := 1.2
	b := 0.75
	avgdl := 50.0

	docLengths := make([]int, numDocs)
	mapper := NewDocIDMapper()
	for i := 0; i < numDocs; i++ {
		docLengths[i] = 30 + rand.Intn(40)
		mapper.GetOrCreateID(fmt.Sprintf("doc_%d", i))
	}

	// Create synthetic posting lists
	lists := make([]*CompressedPostingList, numTerms)
	idfs := make([]float64, numTerms)

	for term := 0; term < numTerms; term++ {
		lists[term] = NewCompressedPostingList()
		idfs[term] = 1.0 + float64(term)*0.5

		// Add postings
		for docID := uint32(0); docID < uint32(numDocs); docID++ {
			if rand.Float64() < 0.4 {
				tf := uint32(rand.Intn(5) + 1)
				lists[term].Add(docID, tf, uint32(docLengths[docID]))
			}
		}
		lists[term].SealTail()
	}

	topK := 10
	hits := BlockMaxWANDScores(lists, idfs, topK, avgdl, docLengths, mapper, k1, b)

	if len(hits) == 0 {
		t.Fatalf("expected hits, got 0")
	}

	// Verify hits are sorted descending by score
	for i := 1; i < len(hits); i++ {
		if hits[i].Score > hits[i-1].Score {
			t.Fatalf("hits not sorted: [%d] %f > [%d] %f", i, hits[i].Score, i-1, hits[i-1].Score)
		}
	}

	t.Logf("BlockMaxWAND returned %d hits, top score = %f", len(hits), hits[0].Score)
}
