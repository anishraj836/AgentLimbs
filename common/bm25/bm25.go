package bm25

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/crawler-monorepo/common/index"
	"github.com/crawler-monorepo/common/stemmer"
	"github.com/crawler-monorepo/common/stopwords"
)

const (
	k1 = 1.2  // Term frequency saturation parameter
	b  = 0.75 // Document length normalization parameter
)

// SearchHit represents a single ranked document match.
type SearchHit struct {
	DocID     string  `json:"doc_id"`
	Score     float64 `json:"score"`
	Title     string  `json:"title"`
	URL       string  `json:"url"`
	Snippet   string  `json:"snippet"`
	MatchCount int    `json:"match_count"`
}

// ComputeIDF calculates Okapi BM25 Inverse Document Frequency from scratch.
// IDF(q) = ln( (N - n(q) + 0.5) / (n(q) + 0.5) + 1 )
func ComputeIDF(totalDocs int64, docFreq int) float64 {
	if totalDocs <= 0 || docFreq <= 0 {
		return 0.0
	}
	N := float64(totalDocs)
	n := float64(docFreq)
	num := N - n + 0.5
	den := n + 0.5
	if num < 0 {
		num = 0.5
	}
	return math.Log((num / den) + 1.0)
}

// ComputeBM25Score computes the Okapi BM25 score for a term in a document.
// Score(q, D) = IDF(q) * [ (f(q,D) * (k1 + 1)) / (f(q,D) + k1 * (1 - b + b * (|D| / avgdl))) ]
func ComputeBM25Score(tf int, docLen int, avgDocLen float64, idf float64) float64 {
	if tf <= 0 || idf <= 0 {
		return 0.0
	}
	if avgDocLen <= 0 {
		avgDocLen = 1.0
	}
	lenNorm := 1.0 - b + b*(float64(docLen)/avgDocLen)
	num := float64(tf) * (k1 + 1.0)
	den := float64(tf) + k1*lenNorm
	return idf * (num / den)
}

// RankDocuments evaluates a list of query tokens against an inverted index using BM25.
func RankDocuments(
	query string,
	invIndex *index.InvertedIndex,
	docTitles map[string]string,
	docURLs map[string]string,
	docBodies map[string]string,
	topK int,
) []SearchHit {
	if strings.TrimSpace(query) == "" {
		return nil
	}

	rawTokens := strings.Fields(strings.ToLower(query))
	var stemmedTokens []string
	var queryTerms []string

	for _, t := range rawTokens {
		t = strings.Trim(t, ".,!?:;\"'()[]{}")
		if t == "" || stopwords.IsStopword(t) {
			continue
		}
		stemmed := stemmer.Stem(t)
		stemmedTokens = append(stemmedTokens, stemmed)
		queryTerms = append(queryTerms, t)
	}

	if len(stemmedTokens) == 0 {
		return nil
	}

	totalDocs, avgDocLen, _ := invIndex.GetStats()
	if totalDocs == 0 {
		return nil
	}

	docScores := make(map[string]float64)
	docMatchCounts := make(map[string]int)

	// Calculate BM25 scores across all query terms
	for _, term := range stemmedTokens {
		pl, exists := invIndex.GetPostingList(term)
		if !exists {
			continue
		}

		idf := ComputeIDF(totalDocs, pl.DocumentFrequency)
		for _, entry := range pl.Entries {
			docLen := invIndex.GetDocLength(entry.DocID)
			score := ComputeBM25Score(entry.TermFrequency, docLen, avgDocLen, idf)
			docScores[entry.DocID] += score
			docMatchCounts[entry.DocID]++
		}
	}

	var hits []SearchHit
	for docID, score := range docScores {
		body := docBodies[docID]
		snippet := GenerateHighlightedSnippet(body, queryTerms, 180)

		title := docTitles[docID]
		if title == "" {
			title = docID
		}
		url := docURLs[docID]

		hits = append(hits, SearchHit{
			DocID:      docID,
			Score:      math.Round(score*1000) / 1000,
			Title:      title,
			URL:        url,
			Snippet:    snippet,
			MatchCount: docMatchCounts[docID],
		})
	}

	// Sort hits by BM25 Score descending
	sort.Slice(hits, func(i, j int) bool {
		return hits[i].Score > hits[j].Score
	})

	if len(hits) > topK {
		hits = hits[:topK]
	}

	return hits
}

// GenerateHighlightedSnippet extracts a window of text surrounding query matches and wraps matched words in <mark> tags.
func GenerateHighlightedSnippet(body string, queryTerms []string, maxLen int) string {
	if strings.TrimSpace(body) == "" {
		return ""
	}

	words := strings.Fields(body)
	if len(words) == 0 {
		return ""
	}

	// Find index of first query term match
	matchIdx := 0
	lowerBody := strings.ToLower(body)
	firstMatchPos := len(lowerBody)

	for _, q := range queryTerms {
		pos := strings.Index(lowerBody, strings.ToLower(q))
		if pos != -1 && pos < firstMatchPos {
			firstMatchPos = pos
		}
	}

	if firstMatchPos != len(lowerBody) {
		// Calculate roughly which word index this match corresponds to
		runningLen := 0
		for i, w := range words {
			runningLen += len(w) + 1
			if runningLen >= firstMatchPos {
				matchIdx = i
				break
			}
		}
	}

	// Determine snippet window range
	startIdx := matchIdx - 5
	if startIdx < 0 {
		startIdx = 0
	}
	endIdx := startIdx + 25
	if endIdx > len(words) {
		endIdx = len(words)
	}

	snippetWords := make([]string, 0, endIdx-startIdx)
	for i := startIdx; i < endIdx; i++ {
		w := words[i]
		cleanW := strings.Trim(strings.ToLower(w), ".,!?:;\"'()[]{}")
		matched := false
		for _, q := range queryTerms {
			if cleanW == strings.ToLower(q) || stemmer.Stem(cleanW) == stemmer.Stem(q) {
				matched = true
				break
			}
		}

		if matched {
			snippetWords = append(snippetWords, fmt.Sprintf("<mark>%s</mark>", w))
		} else {
			snippetWords = append(snippetWords, w)
		}
	}

	result := strings.Join(snippetWords, " ")
	if startIdx > 0 {
		result = "..." + result
	}
	if endIdx < len(words) {
		result = result + "..."
	}

	runes := []rune(result)
	if len(runes) > maxLen {
		result = string(runes[:maxLen]) + "..."
	}

	return result
}
