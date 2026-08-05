package hybrid

import (
	"testing"

	"github.com/crawler-monorepo/common/bm25"
	"github.com/crawler-monorepo/common/vector"
)

func TestReciprocalRankFusion(t *testing.T) {
	bm25Hits := []bm25.SearchHit{
		{DocID: "docA", Score: 2.5, Title: "Doc A"},
		{DocID: "docB", Score: 1.8, Title: "Doc B"},
	}

	vectorHits := []vector.VectorSearchResult{
		{DocID: "docB", Similarity: 0.95},
		{DocID: "docA", Similarity: 0.80},
	}

	fused := ReciprocalRankFusion(bm25Hits, vectorHits, 2)
	if len(fused) == 0 {
		t.Fatalf("expected fused hits, got none")
	}

	for _, hit := range fused {
		if hit.RRFScore <= 0 {
			t.Errorf("expected positive RRF score for %s, got %f", hit.DocID, hit.RRFScore)
		}
	}
}
