package tokenizer

import (
	"encoding/json"
	"strings"
	"unicode"

	"github.com/crawler-monorepo/common/stemmer"
	"github.com/crawler-monorepo/common/stopwords"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// TokenizedDocument represents a tokenized document with position indexing.
type TokenizedDocument struct {
	URL           string           `json:"url"`
	Title         string           `json:"title"`
	CleanBody     string           `json:"clean_body"`
	TotalTokens   int              `json:"total_tokens"`
	TermPositions map[string][]int `json:"term_positions"` // stemmed term -> []positions
}

// TokenizePipeline executes: Unicode Norm -> Lowercase -> Remove Punctuation -> Stopword Filter -> Stemming -> Position Indexing.
func TokenizePipeline(url, title, body string) *TokenizedDocument {
	// 1. Unicode Normalization (NFC)
	normBody, _, _ := transform.String(norm.NFC, body)

	// 2. Word Splitting & Lowercasing
	rawWords := strings.Fields(normBody)

	termPositions := make(map[string][]int)
	validTokenCount := 0

	for pos, rawWord := range rawWords {
		// 3. Remove Punctuation
		cleanWord := strings.TrimFunc(rawWord, func(r rune) bool {
			return unicode.IsPunct(r) || unicode.IsSymbol(r)
		})
		cleanWord = strings.ToLower(cleanWord)

		if len(cleanWord) <= 1 {
			continue
		}

		// 4. Stopword Removal
		if stopwords.IsStopword(cleanWord) {
			continue
		}

		// 5. Porter Stemming
		stemmed := stemmer.Stem(cleanWord)
		if stemmed == "" {
			continue
		}

		// 6. Record Position Index
		termPositions[stemmed] = append(termPositions[stemmed], pos)
		validTokenCount++
	}

	return &TokenizedDocument{
		URL:           url,
		Title:         title,
		CleanBody:     body,
		TotalTokens:   validTokenCount,
		TermPositions: termPositions,
	}
}

func (t *TokenizedDocument) SerializeJSON() ([]byte, error) {
	return json.Marshal(t)
}
