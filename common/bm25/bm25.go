package bm25

import (
	"github.com/crawler-monorepo/internal/index"
)

type SearchHit = index.SearchHit

func ComputeIDF(totalDocs int64, docFreq int) float64 {
	return index.ComputeIDF(totalDocs, docFreq)
}

func ComputeBM25Score(tf int, docLen int, avgDocLen float64, idf float64) float64 {
	return index.ComputeBM25Score(tf, docLen, avgDocLen, idf)
}

func RankDocuments(
	query string,
	invIndex *index.InvertedIndex,
	docTitles map[string]string,
	docURLs map[string]string,
	docBodies map[string]string,
	topK int,
) []SearchHit {
	return index.RankDocuments(query, invIndex, docTitles, docURLs, docBodies, topK)
}

func GenerateHighlightedSnippet(body string, queryTerms []string, maxLen int) string {
	return index.GenerateHighlightedSnippet(body, queryTerms, maxLen)
}
