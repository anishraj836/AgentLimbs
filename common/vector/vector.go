package vector

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/crawler-monorepo/common/stopwords"
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

// hashFNV1a computes a 64-bit FNV-1a hash value for string s given a seed offset basis.
func hashFNV1a(s string, seed uint64) uint64 {
	h := seed
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
}

// GenerateFeatureVector produces a deterministic D-dimensional normalized embedding vector
// from text using subword N-gram (3-gram and 4-gram) feature extraction, full word tokens,
// basic stopword filtration, dual-hash projection, and L2 normalization.
func GenerateFeatureVector(text string, dimensions int) []float64 {
	if dimensions <= 0 {
		dimensions = 128
	}

	vec := make([]float64, dimensions)
	rawWords := strings.Fields(strings.ToLower(text))
	if len(rawWords) == 0 {
		return vec
	}

	var features []string
	for _, raw := range rawWords {
		w := strings.Trim(raw, ".,!?:;\"'()[]{}<>-")
		if w == "" || stopwords.IsStopword(w) {
			continue
		}

		// Full word token
		features = append(features, w)

		// Character 3-grams & 4-grams (e.g. "goroutine" -> "gor", "oru", "rut", "uti", "tin", "ine")
		runes := []rune(w)
		n := len(runes)
		for i := 0; i <= n-3; i++ {
			features = append(features, string(runes[i:i+3]))
		}
		for i := 0; i <= n-4; i++ {
			features = append(features, string(runes[i:i+4]))
		}
	}

	if len(features) == 0 {
		return vec
	}

	for _, f := range features {
		// Dual-hash projection: h1 determines target dimension, h2 determines sign (+1 / -1)
		h1 := hashFNV1a(f, 14695981039346656037)
		h2 := hashFNV1a(f, 0xcbf29ce484222325)

		idx := int(h1 % uint64(dimensions))
		sign := 1.0
		if (h2 & 1) != 0 {
			sign = -1.0
		}
		vec[idx] += sign
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
