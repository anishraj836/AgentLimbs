package rerank

import (
	"github.com/crawler-monorepo/common/hybrid"
	"github.com/crawler-monorepo/internal/search"
)

type RerankedHit = search.RerankedHit

// ComputeKeywordTitleBoostScore computes exact keyword and title frequency boosts for post-RRF search candidate re-ranking.
func ComputeKeywordTitleBoostScore(query string, title string, snippet string) float64 {
	return search.ComputeKeywordTitleBoostScore(query, title, snippet)
}

func RerankCandidates(query string, candidateHits []hybrid.HybridSearchHit, topK int) []RerankedHit {
	return search.RerankCandidates(query, candidateHits, topK)
}
