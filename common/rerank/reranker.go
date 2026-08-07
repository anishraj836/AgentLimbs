package rerank

import (
	"github.com/crawler-monorepo/common/hybrid"
	"github.com/crawler-monorepo/internal/search"
)

type RerankedHit = search.RerankedHit

func ComputeCrossEncoderScore(query string, title string, snippet string) float64 {
	return search.ComputeCrossEncoderScore(query, title, snippet)
}

func RerankCandidates(query string, candidateHits []hybrid.HybridSearchHit, topK int) []RerankedHit {
	return search.RerankCandidates(query, candidateHits, topK)
}
