package search

import (
	"math"
	"sort"
	"strings"

	"github.com/crawler-monorepo/common/stopwords"
	"github.com/crawler-monorepo/internal/index"
)

// RRFConstant is the smoothing constant k used in reciprocal rank calculation: 1.0 / (k + rank).
const RRFConstant = 60.0

// HybridSearchHit represents a search result combined from lexical and vector indexes.
type HybridSearchHit struct {
	DocID      string  `json:"doc_id"`
	RRFScore   float64 `json:"rrf_score"`
	BM25Score  float64 `json:"bm25_score"`
	VectorSim  float64 `json:"vector_similarity"`
	BM25Rank   *int    `json:"bm25_rank"`
	VectorRank *int    `json:"vector_rank"`
	Title      string  `json:"title"`
	URL        string  `json:"url"`
	SourceType string  `json:"source_type"`
	SourceURL  string  `json:"source_url"`
	Snippet    string  `json:"snippet"`
}

// ReciprocalRankFusion merges and ranks BM25 lexical results and dense vector results into a unified list.
func ReciprocalRankFusion(
	query string,
	bm25Hits []index.SearchHit,
	vectorHits []index.VectorSearchResult,
	topK int,
	metadataMaps ...map[string]string,
) []HybridSearchHit {
	if topK <= 0 {
		topK = 5
	}

	var docTitles, docURLs, docBodies, docSourceTypes, docSourceURLs map[string]string
	if len(metadataMaps) > 0 {
		docTitles = metadataMaps[0]
	}
	if len(metadataMaps) > 1 {
		docURLs = metadataMaps[1]
	}
	if len(metadataMaps) > 2 {
		docBodies = metadataMaps[2]
	}
	if len(metadataMaps) > 3 {
		docSourceTypes = metadataMaps[3]
	}
	if len(metadataMaps) > 4 {
		docSourceURLs = metadataMaps[4]
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

	allDocIDs := make(map[string]bool)
	for docID := range bm25Ranks {
		allDocIDs[docID] = true
	}
	for docID := range vectorRanks {
		allDocIDs[docID] = true
	}

	type candidateRRF struct {
		docID      string
		rawRRF     float64
		bm25Score  float64
		vectorSim  float64
		bmRankPtr  *int
		vecRankPtr *int
		title      string
		url        string
		sourceType string
		sourceURL  string
		snippet    string
	}

	var candidates []candidateRRF

	for docID := range allDocIDs {
		var score float64

		var bmRankPtr *int
		bmRank, inBM25 := bm25Ranks[docID]
		if inBM25 && bm25Scores[docID] > 0 {
			score += 1.0 / (RRFConstant + float64(bmRank))
			r := bmRank
			bmRankPtr = &r
		}

		var vecRankPtr *int
		vecRank, inVec := vectorRanks[docID]
		if inVec {
			score += 1.0 / (RRFConstant + float64(vecRank))
			r := vecRank
			vecRankPtr = &r
		}

		if score <= 0.000001 {
			continue
		}

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
			if docTitles != nil {
				if t, ok := docTitles[docID]; ok && t != "" {
					title = t
				}
			}
			if docURLs != nil {
				if u, ok := docURLs[docID]; ok && u != "" {
					url = u
				}
			}
			if docBodies != nil {
				if b, ok := docBodies[docID]; ok && b != "" {
					snippet = index.GenerateHighlightedSnippet(b, nil, 180)
				}
			}
		}

		sourceType := ""
		sourceURL := ""
		if docSourceTypes != nil {
			if st, ok := docSourceTypes[docID]; ok && st != "" {
				sourceType = st
			}
		}
		if docSourceURLs != nil {
			if su, ok := docSourceURLs[docID]; ok && su != "" {
				sourceURL = su
			}
		}
		if sourceType == "" {
			sourceType = "web_crawled"
		}
		if sourceURL == "" {
			if url != "" {
				sourceURL = url
			} else {
				sourceURL = docID
			}
		}

		candidates = append(candidates, candidateRRF{
			docID:      docID,
			rawRRF:     score,
			bm25Score:  bm25Scores[docID],
			vectorSim:  vectorSims[docID],
			bmRankPtr:  bmRankPtr,
			vecRankPtr: vecRankPtr,
			title:      title,
			url:        url,
			sourceType: sourceType,
			sourceURL:  sourceURL,
			snippet:    snippet,
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		if math.Abs(candidates[i].rawRRF-candidates[j].rawRRF) < 1e-9 {
			return candidates[i].docID < candidates[j].docID
		}
		return candidates[i].rawRRF > candidates[j].rawRRF
	})

	if len(candidates) > topK {
		candidates = candidates[:topK]
	}

	fusedHits := make([]HybridSearchHit, 0, len(candidates))
	for _, c := range candidates {
		fusedHits = append(fusedHits, HybridSearchHit{
			DocID:      c.docID,
			RRFScore:   math.Round(c.rawRRF*100000) / 100000,
			BM25Score:  c.bm25Score,
			VectorSim:  c.vectorSim,
			BM25Rank:   c.bmRankPtr,
			VectorRank: c.vecRankPtr,
			Title:      c.title,
			URL:        c.url,
			SourceType: c.sourceType,
			SourceURL:  c.sourceURL,
			Snippet:    c.snippet,
		})
	}

	return fusedHits
}

// RerankedHit represents a search hit after secondary scoring and position adjustment.
type RerankedHit struct {
	DocID        string  `json:"doc_id"`
	RerankScore  float64 `json:"rerank_score"`
	OriginalRank int     `json:"original_rank"`
	Title        string  `json:"title"`
	URL          string  `json:"url"`
	Snippet      string  `json:"snippet"`
}

// ComputeKeywordTitleBoostScore calculates boost points for exact query terms appearing in document title and snippet.
func ComputeKeywordTitleBoostScore(query string, title string, snippet string) float64 {
	qWords := strings.Fields(strings.ToLower(query))
	if len(qWords) == 0 {
		return 0.0
	}

	docText := strings.ToLower(title + " " + snippet)

	var score float64
	for _, qw := range qWords {
		qw = strings.Trim(qw, ".,!?:;\"'()[]{}")
		if qw == "" || stopwords.IsStopword(qw) {
			continue
		}
		if strings.Contains(docText, qw) {
			score += 1.5
		}
	}

	titleLower := strings.ToLower(title)
	for _, qw := range qWords {
		qw = strings.Trim(qw, ".,!?:;\"'()[]{}")
		if qw == "" || stopwords.IsStopword(qw) {
			continue
		}
		if strings.Contains(titleLower, qw) {
			score += 2.0
		}
	}

	return math.Round(score*100) / 100
}

// RerankCandidates scores candidate hits by combining their RRF rank score with title and snippet keyword boosts.
func RerankCandidates(query string, candidateHits []HybridSearchHit, topK int) []RerankedHit {
	if len(candidateHits) == 0 || topK <= 0 {
		return nil
	}

	type candidateRerank struct {
		hit          HybridSearchHit
		rawScore     float64
		originalRank int
	}

	var candidates []candidateRerank
	for i, hit := range candidateHits {
		boostScore := ComputeKeywordTitleBoostScore(query, hit.Title, hit.Snippet)
		finalScore := (hit.RRFScore * 100.0) + boostScore

		candidates = append(candidates, candidateRerank{
			hit:          hit,
			rawScore:     finalScore,
			originalRank: i + 1,
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		if math.Abs(candidates[i].rawScore-candidates[j].rawScore) < 1e-9 {
			return candidates[i].hit.DocID < candidates[j].hit.DocID
		}
		return candidates[i].rawScore > candidates[j].rawScore
	})

	if len(candidates) > topK {
		candidates = candidates[:topK]
	}

	var reranked []RerankedHit
	for _, c := range candidates {
		reranked = append(reranked, RerankedHit{
			DocID:        c.hit.DocID,
			RerankScore:  math.Round(c.rawScore*1000) / 1000,
			OriginalRank: c.originalRank,
			Title:        c.hit.Title,
			URL:          c.hit.URL,
			Snippet:      c.hit.Snippet,
		})
	}

	return reranked
}
