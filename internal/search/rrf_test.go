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

	fused := ReciprocalRankFusion("", bm25Hits, vectorHits, 5)

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

func TestUnrankedRanksJSONSchema(t *testing.T) {
	bm25Hits := []index.SearchHit{
		{DocID: "doc1", Score: 2.5, Title: "Doc 1"},
	}

	vectorHits := []index.VectorSearchResult{
		{DocID: "doc3", Similarity: 0.85},
	}

	fused := ReciprocalRankFusion("", bm25Hits, vectorHits, 5)

	var doc1Hit, doc3Hit *HybridSearchHit
	for i := range fused {
		if fused[i].DocID == "doc1" {
			doc1Hit = &fused[i]
		} else if fused[i].DocID == "doc3" {
			doc3Hit = &fused[i]
		}
	}

	if doc1Hit == nil || doc3Hit == nil {
		t.Fatalf("Expected both doc1 and doc3 in fused hits")
	}

	// doc1 matched BM25, unranked in Vector
	if doc1Hit.BM25Rank == nil || *doc1Hit.BM25Rank != 1 {
		t.Errorf("Expected doc1 BM25Rank to be &1, got %v", doc1Hit.BM25Rank)
	}
	if doc1Hit.VectorRank != nil {
		t.Errorf("Expected doc1 VectorRank to be nil (null in JSON), got %v", *doc1Hit.VectorRank)
	}

	// doc3 unranked in BM25, matched Vector
	if doc3Hit.BM25Rank != nil {
		t.Errorf("Expected doc3 BM25Rank to be nil (null in JSON), got %v", *doc3Hit.BM25Rank)
	}
	if doc3Hit.VectorRank == nil || *doc3Hit.VectorRank != 1 {
		t.Errorf("Expected doc3 VectorRank to be &1, got %v", doc3Hit.VectorRank)
	}
}

func TestRRFScoreScalingAndNormalization(t *testing.T) {
	bm25Hits := []index.SearchHit{
		{DocID: "doc1", Score: 10.0, Title: "Golang Distributed Crawler", Snippet: "High performance crawling engine in Go"},
		{DocID: "doc2", Score: 5.0, Title: "Python Scraper", Snippet: "Scraping with BeautifulSoup"},
	}

	vectorHits := []index.VectorSearchResult{
		{DocID: "doc1", Similarity: 0.98},
		{DocID: "doc3", Similarity: 0.75},
	}

	// Test with query - RRFScore must remain pure reciprocal rank score without integer bonuses (+1.5, +2.0)
	fused := ReciprocalRankFusion("Golang Distributed Crawler", bm25Hits, vectorHits, 5)

	if len(fused) != 3 {
		t.Fatalf("Expected 3 fused hits, got %d", len(fused))
	}

	// Maximum possible RRF score for rank 1 in both BM25 and Vector is 1/(60+1) + 1/(60+1) = 2/61 ≈ 0.0327868...
	for _, hit := range fused {
		if hit.RRFScore > 0.033 || hit.RRFScore < 0.0 {
			t.Errorf("Doc %s RRFScore %f is out of expected pure RRF range [0.0, 0.033]", hit.DocID, hit.RRFScore)
		}
	}

	// doc1 has rank 1 in both BM25 and Vector: 1/61 + 1/61 = 0.0327868... -> ~0.03279
	expectedDoc1Score := 1.0/61.0 + 1.0/61.0
	diff := hitScoreDiff(fused[0].RRFScore, expectedDoc1Score)
	if diff > 0.0001 {
		t.Errorf("Expected doc1 pure RRFScore ~%f, got %f (diff: %f)", expectedDoc1Score, fused[0].RRFScore, diff)
	}
}

func hitScoreDiff(a, b float64) float64 {
	if a > b {
		return a - b
	}
	return b - a
}

