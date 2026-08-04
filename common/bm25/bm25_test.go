package bm25

import (
	"math"
	"testing"

	"github.com/crawler-monorepo/common/index"
)

func TestBM25Ranking(t *testing.T) {
	invIndex := index.NewInvertedIndex()

	// Add Document 1: "Golang is a fast compiled programming language"
	invIndex.AddDocument("doc1", map[string][]int{
		"go":         {0},
		"lang":       {0},
		"fast":       {2},
		"compil":     {3},
		"program":    {4},
		"languag":    {5},
	}, 6)

	// Add Document 2: "Python is a dynamic interpreted programming language"
	invIndex.AddDocument("doc2", map[string][]int{
		"python":     {0},
		"dynam":      {2},
		"interpret":  {3},
		"program":    {4},
		"languag":    {5},
	}, 6)

	titles := map[string]string{
		"doc1": "Go Programming",
		"doc2": "Python Programming",
	}
	urls := map[string]string{
		"doc1": "https://golang.org",
		"doc2": "https://python.org",
	}
	bodies := map[string]string{
		"doc1": "Golang is a fast compiled programming language for high performance systems.",
		"doc2": "Python is a dynamic interpreted programming language widely used in AI.",
	}

	hits := RankDocuments("fast compiled golang", invIndex, titles, urls, bodies, 10)
	if len(hits) == 0 {
		t.Fatalf("expected search hits for query 'fast compiled golang', got none")
	}

	if hits[0].DocID != "doc1" {
		t.Errorf("expected doc1 to rank highest for 'fast compiled golang', got %s", hits[0].DocID)
	}

	if math.IsNaN(hits[0].Score) || hits[0].Score <= 0 {
		t.Errorf("expected positive BM25 score, got %f", hits[0].Score)
	}
}

func TestGenerateHighlightedSnippet(t *testing.T) {
	body := "The Go programming language is an open source project to make programmers more productive."
	queryTerms := []string{"programming", "language"}

	snippet := GenerateHighlightedSnippet(body, queryTerms, 100)
	if snippet == "" {
		t.Fatalf("expected non-empty snippet")
	}

	if !testing.Short() {
		t.Logf("Generated snippet: %s", snippet)
	}
}
