package search

import (
	"testing"

	"github.com/crawler-monorepo/internal/index"
)

func TestReciprocalRankFusion(t *testing.T) {
	bm25Hits := []index.SearchHit{
		{DocID: "doc1", Score: 2.5, Title: "Doc 1", Snippet: "First doc snippet"},
		{DocID: "doc2", Score: 1.8, Title: "Doc 2", Snippet: "Second doc snippet"},
	}

	vectorHits := []index.VectorSearchResult{
		{DocID: "doc2", Similarity: 0.95},
		{DocID: "doc3", Similarity: 0.85},
	}

	fused := ReciprocalRankFusion(bm25Hits, vectorHits, 5)

	if len(fused) != 3 {
		t.Fatalf("Expected 3 fused hits, got %d", len(fused))
	}

	// doc2 is in both BM25 and Vector, so it should rank highest in RRF
	if fused[0].DocID != "doc2" {
		t.Errorf("Expected top fused doc to be 'doc2', got '%s'", fused[0].DocID)
	}
}

func TestKeywordTitleBoostReranking(t *testing.T) {
	query := "golang web crawler"

	candidates := []HybridSearchHit{
		{DocID: "doc1", RRFScore: 0.03, Title: "Java Framework", Snippet: "Overview of Java ecosystem"},
		{DocID: "doc2", RRFScore: 0.02, Title: "Golang Web Crawler Engine", Snippet: "Build a high performance golang web crawler"},
	}

	reranked := RerankCandidates(query, candidates, 5)

	if len(reranked) != 2 {
		t.Fatalf("Expected 2 reranked hits, got %d", len(reranked))
	}

	if reranked[0].DocID != "doc2" {
		t.Errorf("Expected 'doc2' to rank first due to title & snippet match boost, got '%s'", reranked[0].DocID)
	}
}
