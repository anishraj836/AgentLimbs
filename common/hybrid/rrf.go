package hybrid

import (
	"math"
	"sort"

	"github.com/crawler-monorepo/common/bm25"
	"github.com/crawler-monorepo/common/vector"
)

const RRFConstant = 60.0 // Standard Reciprocal Rank Fusion constant k=60

// HybridSearchHit represents a fused search hit with both BM25 and Vector semantic ranks.
type HybridSearchHit struct {
	DocID       string  `json:"doc_id"`
	RRFScore    float64 `json:"rrf_score"`
	BM25Score   float64 `json:"bm25_score"`
	VectorSim   float64 `json:"vector_similarity"`
	BM25Rank    int     `json:"bm25_rank"`
	VectorRank  int     `json:"vector_rank"`
	Title       string  `json:"title"`
	URL         string  `json:"url"`
	Snippet     string  `json:"snippet"`
}

// ReciprocalRankFusion merges sparse BM25 hits and dense Vector search hits using RRF.
// RRF_Score(d) = 1 / (k + rank_bm25(d)) + 1 / (k + rank_vector(d))
func ReciprocalRankFusion(
	bm25Hits []bm25.SearchHit,
	vectorHits []vector.VectorSearchResult,
	topK int,
) []HybridSearchHit {
	if topK <= 0 {
		topK = 5
	}

	bm25Ranks := make(map[string]int)
	bm25Scores := make(map[string]float64)
	bm25HitsMap := make(map[string]bm25.SearchHit)

	for i, hit := range bm25Hits {
		rank := i + 1
		bm25Ranks[hit.DocID] = rank
		bm25Scores[hit.DocID] = hit.Score
		bm25HitsMap[hit.DocID] = hit
	}

	vectorRanks := make(map[string]int)
	vectorSims := make(map[string]float64)

	for i, hit := range vectorHits {
		rank := i + 1
		vectorRanks[hit.DocID] = rank
		vectorSims[hit.DocID] = hit.Similarity
	}

	// Calculate RRF score across union of docIDs
	rrfScores := make(map[string]float64)
	allDocIDs := make(map[string]bool)

	for docID := range bm25Ranks {
		allDocIDs[docID] = true
	}
	for docID := range vectorRanks {
		allDocIDs[docID] = true
	}

	var fusedHits []HybridSearchHit

	for docID := range allDocIDs {
		var score float64

		bmRank, inBM25 := bm25Ranks[docID]
		if inBM25 {
			score += 1.0 / (RRFConstant + float64(bmRank))
		}

		vecRank, inVec := vectorRanks[docID]
		if inVec {
			score += 1.0 / (RRFConstant + float64(vecRank))
		}

		rrfScores[docID] = score

		title := ""
		url := ""
		snippet := ""
		if hit, exists := bm25HitsMap[docID]; exists {
			title = hit.Title
			url = hit.URL
			snippet = hit.Snippet
		} else {
			title = docID
			url = docID
		}

		fusedHits = append(fusedHits, HybridSearchHit{
			DocID:       docID,
			RRFScore:    math.Round(score*100000) / 100000,
			BM25Score:   bm25Scores[docID],
			VectorSim:   vectorSims[docID],
			BM25Rank:    bmRank,
			VectorRank:  vecRank,
			Title:       title,
			URL:         url,
			Snippet:     snippet,
		})
	}

	// Sort by RRFScore descending
	sort.Slice(fusedHits, func(i, j int) bool {
		return fusedHits[i].RRFScore > fusedHits[j].RRFScore
	})

	if len(fusedHits) > topK {
		fusedHits = fusedHits[:topK]
	}

	return fusedHits
}
