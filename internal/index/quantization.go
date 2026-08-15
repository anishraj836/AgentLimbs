package index

import (
	"fmt"
	"math"
)

// VectorPrecision specifies the storage and compute precision of dense vectors in memory.
type VectorPrecision string

const (
	PrecisionFloat64 VectorPrecision = "float64"
	PrecisionFloat32 VectorPrecision = "float32"
	PrecisionInt8    VectorPrecision = "int8"
)

// QuantizedVector stores a symmetrically quantized 8-bit signed integer vector with a per-vector scaling factor.
type QuantizedVector struct {
	Data  []int8  `json:"d"`
	Scale float32 `json:"s"`
}

// NormalizeL2Float64 calculates the Euclidean L2 norm of a float64 slice and returns a normalized copy.
func NormalizeL2Float64(v []float64) ([]float64, error) {
	if len(v) == 0 {
		return nil, fmt.Errorf("empty vector")
	}

	var sumSq float64
	for i, val := range v {
		if math.IsNaN(val) || math.IsInf(val, 0) {
			return nil, fmt.Errorf("vector contains NaN or Inf at index %d", i)
		}
		sumSq += val * val
	}

	norm := math.Sqrt(sumSq)
	res := make([]float64, len(v))
	if norm == 0 {
		return res, nil
	}

	invNorm := 1.0 / norm
	for i, val := range v {
		res[i] = val * invNorm
	}
	return res, nil
}

// NormalizeL2Float32 calculates the Euclidean L2 norm of a float32 slice and returns a normalized copy.
func NormalizeL2Float32(v []float32) ([]float32, error) {
	if len(v) == 0 {
		return nil, fmt.Errorf("empty vector")
	}

	var sumSq float64
	for i, val := range v {
		fVal := float64(val)
		if math.IsNaN(fVal) || math.IsInf(fVal, 0) {
			return nil, fmt.Errorf("vector contains NaN or Inf at index %d", i)
		}
		sumSq += fVal * fVal
	}

	norm := float32(math.Sqrt(sumSq))
	res := make([]float32, len(v))
	if norm == 0 {
		return res, nil
	}

	invNorm := float32(1.0) / norm
	for i, val := range v {
		res[i] = val * invNorm
	}
	return res, nil
}

// QuantizeVector converts a raw float64 vector into a unit-normalized Int8 QuantizedVector.
func QuantizeVector(v []float64) (QuantizedVector, error) {
	if len(v) == 0 {
		return QuantizedVector{}, fmt.Errorf("empty vector")
	}

	normVec, err := NormalizeL2Float64(v)
	if err != nil {
		return QuantizedVector{}, err
	}

	var maxAbs float64
	for _, val := range normVec {
		absVal := math.Abs(val)
		if absVal > maxAbs {
			maxAbs = absVal
		}
	}

	if maxAbs == 0 {
		return QuantizedVector{
			Data:  make([]int8, len(v)),
			Scale: 1.0,
		}, nil
	}

	scale := float32(maxAbs / 127.0)
	invScale := float64(1.0) / float64(scale)

	data := make([]int8, len(v))
	for i, val := range normVec {
		r := math.Round(val * invScale)
		if r > 127.0 {
			r = 127.0
		} else if r < -127.0 {
			r = -127.0
		}
		data[i] = int8(r)
	}

	return QuantizedVector{
		Data:  data,
		Scale: scale,
	}, nil
}

// DequantizeVector reconstructs an approximate float32 representation from an Int8 QuantizedVector.
func DequantizeVector(qv QuantizedVector) []float32 {
	if len(qv.Data) == 0 {
		return nil
	}
	res := make([]float32, len(qv.Data))
	scale := qv.Scale
	for i, d := range qv.Data {
		res[i] = float32(d) * scale
	}
	return res
}

// DotProductFloat32 computes the inner product between two float32 slices.
func DotProductFloat32(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0.0
	}
	var sum float32
	for i := 0; i < len(a); i++ {
		sum += a[i] * b[i]
	}
	if sum > 1.0 {
		return 1.0
	} else if sum < -1.0 {
		return -1.0
	}
	return sum
}

// DotProductInt8 computes the cosine similarity between a float32 normalized query vector and an Int8 QuantizedVector.
func DotProductInt8(query []float32, doc QuantizedVector) float32 {
	if len(query) != len(doc.Data) || len(query) == 0 {
		return 0.0
	}
	var sum float32
	for i := 0; i < len(query); i++ {
		sum += query[i] * float32(doc.Data[i])
	}
	sim := sum * doc.Scale
	if sim > 1.0 {
		return 1.0
	} else if sim < -1.0 {
		return -1.0
	}
	return sim
}
