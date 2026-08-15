package index

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"testing"
)

func TestNormalizeL2(t *testing.T) {
	vec := []float64{3.0, 4.0}
	norm, err := NormalizeL2Float64(vec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if math.Abs(norm[0]-0.6) > 1e-6 || math.Abs(norm[1]-0.8) > 1e-6 {
		t.Errorf("unexpected normalized vector: %v", norm)
	}

	// Test zero vector
	zeroVec := []float64{0.0, 0.0, 0.0}
	normZero, err := NormalizeL2Float64(zeroVec)
	if err != nil {
		t.Fatalf("unexpected error on zero vector: %v", err)
	}
	if normZero[0] != 0 || normZero[1] != 0 {
		t.Errorf("expected zero vector, got: %v", normZero)
	}

	// Test NaN / Inf detection
	nanVec := []float64{1.0, math.NaN(), 3.0}
	if _, err := NormalizeL2Float64(nanVec); err == nil {
		t.Errorf("expected error on NaN vector, got nil")
	}

	infVec := []float64{1.0, math.Inf(1), 3.0}
	if _, err := NormalizeL2Float64(infVec); err == nil {
		t.Errorf("expected error on Inf vector, got nil")
	}
}

func TestQuantizeAndDequantizeVector(t *testing.T) {
	vec := []float64{0.1, -0.5, 0.8, -0.3, 0.0}
	qv, err := QuantizeVector(vec)
	if err != nil {
		t.Fatalf("QuantizeVector failed: %v", err)
	}

	if len(qv.Data) != len(vec) {
		t.Fatalf("expected data length %d, got %d", len(vec), len(qv.Data))
	}
	if qv.Scale <= 0 {
		t.Errorf("expected positive scale, got %f", qv.Scale)
	}

	deq := DequantizeVector(qv)
	normVec, _ := NormalizeL2Float64(vec)
	for i := range normVec {
		diff := math.Abs(float64(deq[i]) - normVec[i])
		if diff > 0.02 {
			t.Errorf("index %d: large reconstruction error %f (orig %f vs deq %f)", i, diff, normVec[i], deq[i])
		}
	}
}

func TestRecallAt10_SyntheticBenchmark(t *testing.T) {
	dimensions := []int{128, 768, 1536}
	numDocs := 1000
	numQueries := 50
	topK := 10

	rng := rand.New(rand.NewSource(42))

	for _, dim := range dimensions {
		t.Run(fmt.Sprintf("Dim_%d", dim), func(t *testing.T) {
			docsF64 := make([][]float64, numDocs)
			docsInt8 := make([]QuantizedVector, numDocs)
			queriesF64 := make([][]float64, numQueries)
			queriesF32 := make([][]float32, numQueries)

			for i := 0; i < numDocs; i++ {
				v := make([]float64, dim)
				for d := 0; d < dim; d++ {
					v[d] = rng.NormFloat64()
				}
				normV, _ := NormalizeL2Float64(v)
				docsF64[i] = normV
				qv, err := QuantizeVector(normV)
				if err != nil {
					t.Fatalf("quantize error: %v", err)
				}
				docsInt8[i] = qv
			}

			for q := 0; q < numQueries; q++ {
				qv := make([]float64, dim)
				for d := 0; d < dim; d++ {
					qv[d] = rng.NormFloat64()
				}
				normQ, _ := NormalizeL2Float64(qv)
				queriesF64[q] = normQ

				q32 := make([]float32, dim)
				for d := 0; d < dim; d++ {
					q32[d] = float32(normQ[d])
				}
				queriesF32[q] = q32
			}

			totalRecall := 0.0

			for q := 0; q < numQueries; q++ {
				type docScore struct {
					id    int
					score float64
				}

				// Exact Float64 Ranking
				f64Scores := make([]docScore, numDocs)
				for i := 0; i < numDocs; i++ {
					var dot float64
					for d := 0; d < dim; d++ {
						dot += queriesF64[q][d] * docsF64[i][d]
					}
					f64Scores[i] = docScore{id: i, score: dot}
				}
				sort.Slice(f64Scores, func(i, j int) bool {
					return f64Scores[i].score > f64Scores[j].score
				})

				// Int8 Quantized Ranking
				int8Scores := make([]docScore, numDocs)
				for i := 0; i < numDocs; i++ {
					sim := DotProductInt8(queriesF32[q], docsInt8[i])
					int8Scores[i] = docScore{id: i, score: float64(sim)}
				}
				sort.Slice(int8Scores, func(i, j int) bool {
					return int8Scores[i].score > int8Scores[j].score
				})

				// Compute Top-K Intersection
				topF64Set := make(map[int]bool)
				for k := 0; k < topK; k++ {
					topF64Set[f64Scores[k].id] = true
				}

				matched := 0
				for k := 0; k < topK; k++ {
					if topF64Set[int8Scores[k].id] {
						matched++
					}
				}

				recall := float64(matched) / float64(topK)
				totalRecall += recall
			}

			avgRecall := totalRecall / float64(numQueries)
			t.Logf("Dimension %d: Average Recall@10 = %.4f (%.2f%%)", dim, avgRecall, avgRecall*100)

			minExpectedRecall := 0.980
			if avgRecall < minExpectedRecall {
				t.Errorf("dim %d: expected recall >= %f, got %f", dim, minExpectedRecall, avgRecall)
			}
		})
	}
}
