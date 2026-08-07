package vector

import (
	"github.com/crawler-monorepo/internal/index"
)

type VectorSearchResult = index.VectorSearchResult
type VectorIndex = index.VectorIndex

func CosineSimilarity(u, v []float64) float64 {
	return index.CosineSimilarity(u, v)
}

func GenerateFeatureVector(text string, dimensions int) []float64 {
	return index.GenerateFeatureVector(text, dimensions)
}

func NewVectorIndex(dimensions int) *VectorIndex {
	return index.NewVectorIndex(dimensions)
}
