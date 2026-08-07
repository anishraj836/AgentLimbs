package search

import (
	"math"
	"sort"
	"strings"

	"github.com/crawler-monorepo/internal/index"
)

const RRFConstant = 60.0

type HybridSearchHit struct {
	DocID       string  `json:"doc_id"`
	RRFScore    float64 `json:"rrf_score"`
	BM25Score   float64 `json:"bm25_score"`
	VectorSim   float64 `json:"vector_similarity"`
	BM25Rank    int     `json:"bm25_rank"`
	VectorRank  int     `json:"vector_rank"`
	Title       string  `json:"title"`
	URL         string  `json:"url"`
	SourceType  string  `json:"source_type"`
	SourceURL   string  `json:"source_url"`
	Snippet     string  `json:"snippet"`
}

func ReciprocalRankFusion(
	bm25Hits []index.SearchHit,
	vectorHits []index.VectorSearchResult,
	topK int,
) []HybridSearchHit {
	if topK <= 0 {
		topK = 5
	}

	bm25Ranks := make(map[string]int)
	bm25Scores := make(map[string]float64)
	bm25HitsMap := make(map[string]index.SearchHit)

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
			DocID:      docID,
			RRFScore:   math.Round(score*100000) / 100000,
			BM25Score:  bm25Scores[docID],
			VectorSim:  vectorSims[docID],
			BM25Rank:   bmRank,
			VectorRank: vecRank,
			Title:      title,
			URL:        url,
			Snippet:    snippet,
		})
	}

	sort.Slice(fusedHits, func(i, j int) bool {
		return fusedHits[i].RRFScore > fusedHits[j].RRFScore
	})

	if len(fusedHits) > topK {
		fusedHits = fusedHits[:topK]
	}

	return fusedHits
}

// Cross-Encoder Contextual Score Calculation

type RerankedHit struct {
	DocID        string  `json:"doc_id"`
	RerankScore  float64 `json:"rerank_score"`
	OriginalRank int     `json:"original_rank"`
	Title        string  `json:"title"`
	URL          string  `json:"url"`
	Snippet      string  `json:"snippet"`
}

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

	titleLower := strings.ToLower(title)
	for _, qw := range qWords {
		if strings.Contains(titleLower, qw) {
			score += 2.0
		}
	}

	return math.Round(score*100) / 100
}

func RerankCandidates(query string, candidateHits []HybridSearchHit, topK int) []RerankedHit {
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
