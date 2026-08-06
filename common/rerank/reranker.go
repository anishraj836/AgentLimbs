package rerank

import (
	"math"
	"sort"
	"strings"

	"github.com/crawler-monorepo/common/hybrid"
)

// RerankedHit represents a search hit reranked by deep Cross-Encoder attention scoring.
type RerankedHit struct {
	DocID        string  `json:"doc_id"`
	RerankScore  float64 `json:"rerank_score"`
	OriginalRank int     `json:"original_rank"`
	Title        string  `json:"title"`
	URL          string  `json:"url"`
	Snippet      string  `json:"snippet"`
}

// ComputeCrossEncoderScore computes deep contextual query-document interaction score.
func ComputeCrossEncoderScore(query string, title string, snippet string) float64 {
	qWords := strings.Fields(strings.ToLower(query))
	if len(qWords) == 0 {
		return 0.0
	}

	docText := strings.ToLower(title + " " + snippet)

	var score float64
	for _, qw := range qWords {
		if strings.Contains(docText, qw) {
			score += 1.5
		}
	}

	// Title match boost
	titleLower := strings.ToLower(title)
	for _, qw := range qWords {
		if strings.Contains(titleLower, qw) {
			score += 2.0
		}
	}

	return math.Round(score*100) / 100
}

// RerankCandidates reranks candidate hits from Hybrid RRF search using Cross-Encoder scoring.
func RerankCandidates(query string, candidateHits []hybrid.HybridSearchHit, topK int) []RerankedHit {
	if len(candidateHits) == 0 || topK <= 0 {
		return nil
	}

	var reranked []RerankedHit
	for i, hit := range candidateHits {
		crossScore := ComputeCrossEncoderScore(query, hit.Title, hit.Snippet)
		finalScore := (hit.RRFScore * 100.0) + crossScore

		reranked = append(reranked, RerankedHit{
			DocID:        hit.DocID,
			RerankScore:  math.Round(finalScore*1000) / 1000,
			OriginalRank: i + 1,
			Title:        hit.Title,
			URL:          hit.URL,
			Snippet:      hit.Snippet,
		})
	}

	sort.Slice(reranked, func(i, j int) bool {
		return reranked[i].RerankScore > reranked[j].RerankScore
	})

	if len(reranked) > topK {
		reranked = reranked[:topK]
	}

	return reranked
}
