package vector

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
)

// VectorSearchResult represents a semantic vector search hit.
type VectorSearchResult struct {
	DocID      string  `json:"doc_id"`
	Similarity float64 `json:"similarity"`
}

// CosineSimilarity computes the normalized dot product of two vectors u and v.
// CosineSimilarity(u, v) = (u · v) / (||u|| * ||v||)
func CosineSimilarity(u, v []float64) float64 {
	if len(u) == 0 || len(v) == 0 || len(u) != len(v) {
		return 0.0
	}

	var dotProduct float64
	var normU float64
	var normV float64

	for i := 0; i < len(u); i++ {
		dotProduct += u[i] * v[i]
		normU += u[i] * u[i]
		normV += v[i] * v[i]
	}

	if normU == 0 || normV == 0 {
		return 0.0
	}

	return dotProduct / (math.Sqrt(normU) * math.Sqrt(normV))
}

// GenerateFeatureVector produces a deterministic D-dimensional normalized embedding vector
// from text using a frequency hash projection.
func GenerateFeatureVector(text string, dimensions int) []float64 {
	if dimensions <= 0 {
		dimensions = 128
	}

	vec := make([]float64, dimensions)
	words := strings.Fields(strings.ToLower(text))
	if len(words) == 0 {
		return vec
	}

	for _, w := range words {
		// FNV-1a hash variant for uniform bucket distribution
		var hash uint64 = 14695981039346656037
		for i := 0; i < len(w); i++ {
			hash ^= uint64(w[i])
			hash *= 1099511628211
		}

		idx := int(hash % uint64(dimensions))
		vec[idx] += 1.0
	}

	// L2 Normalize the vector
	var norm float64
	for _, val := range vec {
		norm += val * val
	}

	if norm > 0 {
		mag := math.Sqrt(norm)
		for i := 0; i < len(vec); i++ {
			vec[i] /= mag
		}
	}

	return vec
}

// VectorIndex stores document vector embeddings for fast $O(N)$ semantic similarity search.
type VectorIndex struct {
	mu         sync.RWMutex
	dimensions int
	vectors    map[string][]float64 // DocID -> Embedding Vector
}

func NewVectorIndex(dimensions int) *VectorIndex {
	if dimensions <= 0 {
		dimensions = 128
	}
	return &VectorIndex{
		dimensions: dimensions,
		vectors:    make(map[string][]float64),
	}
}

// AddVector indexes a document embedding vector.
func (vi *VectorIndex) AddVector(docID string, vec []float64) error {
	if len(vec) != vi.dimensions {
		return fmt.Errorf("vector dimension mismatch: expected %d, got %d", vi.dimensions, len(vec))
	}

	vi.mu.Lock()
	defer vi.mu.Unlock()
	vi.vectors[docID] = vec
	return nil
}

// SearchNearest returns the Top-K most semantically similar documents to queryVector.
func (vi *VectorIndex) SearchNearest(queryVector []float64, topK int) []VectorSearchResult {
	vi.mu.RLock()
	defer vi.mu.RUnlock()

	if len(queryVector) != vi.dimensions || topK <= 0 {
		return nil
	}

	var results []VectorSearchResult
	for docID, vec := range vi.vectors {
		sim := CosineSimilarity(queryVector, vec)
		if sim > 0 {
			results = append(results, VectorSearchResult{
				DocID:      docID,
				Similarity: math.Round(sim*10000) / 10000,
			})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Similarity > results[j].Similarity
	})

	if len(results) > topK {
		results = results[:topK]
	}

	return results
}

type vectorSnapshot struct {
	Dimensions int                 `json:"dimensions"`
	Vectors    map[string][]float64 `json:"vectors"`
}

// SaveSnapshot serializes document vector embeddings to disk.
func (vi *VectorIndex) SaveSnapshot(filePath string) error {
	vi.mu.RLock()
	snap := vectorSnapshot{
		Dimensions: vi.dimensions,
		Vectors:    vi.vectors,
	}
	data, err := json.Marshal(snap)
	vi.mu.RUnlock()

	if err != nil {
		return err
	}
	return os.WriteFile(filePath, data, 0644)
}

// LoadSnapshot restores vector embeddings state from disk.
func (vi *VectorIndex) LoadSnapshot(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	var snap vectorSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return err
	}

	vi.mu.Lock()
	defer vi.mu.Unlock()
	vi.dimensions = snap.Dimensions
	vi.vectors = snap.Vectors
	return nil
}
