package rerank

import (
	"testing"

	"github.com/crawler-monorepo/common/hybrid"
)

func TestCrossEncoderReranking(t *testing.T) {
	hits := []hybrid.HybridSearchHit{
		{DocID: "doc1", RRFScore: 0.015, Title: "Generic Web Page", Snippet: "some text"},
		{DocID: "doc2", RRFScore: 0.014, Title: "Golang Concurrency Guide", Snippet: "learn goroutines and channels in go"},
	}

	reranked := RerankCandidates("golang concurrency", hits, 2)
	if len(reranked) != 2 {
		t.Fatalf("expected 2 reranked hits, got %d", len(reranked))
	}

	if reranked[0].DocID != "doc2" {
		t.Errorf("expected doc2 (Golang Concurrency Guide) to be reranked to #1, got %s", reranked[0].DocID)
	}
}
