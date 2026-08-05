package embedder

import (
	"github.com/crawler-monorepo/common/vector"
)

var GlobalVectorIndex = vector.NewVectorIndex(128)

// IndexDocumentVector generates an embedding vector for clean document text and indexes it.
func IndexDocumentVector(docID, title, body string) []float64 {
	text := title + " " + body
	vec := vector.GenerateFeatureVector(text, 128)
	GlobalVectorIndex.AddVector(docID, vec)
	return vec
}
