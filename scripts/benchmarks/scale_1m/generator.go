package main

import (
	"fmt"
	"math"
	mrand "math/rand"
	"sort"
)

// Vocabulary size and Zipfian exponent
const (
	VocabSize = 50000
	ZipfS     = 1.07
	MeanDocLen = 250.0
	SigmaDocLen = 0.5
)

// ZipfianGenerator produces synthetic documents with power-law term distributions.
type ZipfianGenerator struct {
	termDict []string
	cdf      []float64
	muLn     float64
}

// NewZipfianGenerator initializes interned vocabulary and static CDF lookup table.
func NewZipfianGenerator() *ZipfianGenerator {
	// Pre-allocate interned vocabulary strings
	dict := make([]string, VocabSize)
	for i := 0; i < VocabSize; i++ {
		dict[i] = fmt.Sprintf("term_%05d", i)
	}

	// Compute generalized harmonic sum H_{V, s}
	harmonicSum := 0.0
	for n := 1; n <= VocabSize; n++ {
		harmonicSum += 1.0 / math.Pow(float64(n), ZipfS)
	}

	// Precompute static Cumulative Distribution Function
	cdf := make([]float64, VocabSize)
	runningSum := 0.0
	for i := 1; i <= VocabSize; i++ {
		runningSum += 1.0 / math.Pow(float64(i), ZipfS)
		cdf[i-1] = runningSum / harmonicSum
	}
	cdf[VocabSize-1] = 1.0 // Ensure exact upper bound

	// Calculate exact log-normal location parameter
	muLn := math.Log(MeanDocLen) - (SigmaDocLen*SigmaDocLen)/2.0

	return &ZipfianGenerator{
		termDict: dict,
		cdf:      cdf,
		muLn:     muLn,
	}
}

// SampleTerm returns an interned term string sampled according to the Zipfian distribution.
func (zg *ZipfianGenerator) SampleTerm(rng *mrand.Rand) string {
	u := rng.Float64()
	idx := sort.Search(VocabSize, func(i int) bool {
		return zg.cdf[i] >= u
	})
	if idx >= VocabSize {
		idx = VocabSize - 1
	}
	return zg.termDict[idx]
}

// SampleDocLength returns a log-normally distributed document token length clamped to [10, 2000].
func (zg *ZipfianGenerator) SampleDocLength(rng *mrand.Rand) int {
	z := rng.NormFloat64()
	lenVal := math.Exp(zg.muLn + SigmaDocLen*z)
	clamped := int(math.Round(lenVal))
	if clamped < 10 {
		clamped = 10
	}
	if clamped > 2000 {
		clamped = 2000
	}
	return clamped
}

// SyntheticDoc represents an in-memory synthetic technical document for benchmarking.
type SyntheticDoc struct {
	DocID         uint32
	URL           string
	Title         string
	CleanBody     string
	TermPositions map[string][]int
	TotalTokens   int
	Vector        []float64
}

// GenerateDocument generates a single synthetic document with isolated PRNG.
func (zg *ZipfianGenerator) GenerateDocument(docID uint32, dim int, rng *mrand.Rand) SyntheticDoc {
	docLen := zg.SampleDocLength(rng)
	termPositions := make(map[string][]int)

	for pos := 0; pos < docLen; pos++ {
		t := zg.SampleTerm(rng)
		termPositions[t] = append(termPositions[t], pos)
	}

	// Generate normalized synthetic vector
	vec := make([]float64, dim)
	normSq := 0.0
	for d := 0; d < dim; d++ {
		val := rng.NormFloat64()
		vec[d] = val
		normSq += val * val
	}
	if normSq > 0 {
		invNorm := 1.0 / math.Sqrt(normSq)
		for d := 0; d < dim; d++ {
			vec[d] *= invNorm
		}
	}

	url := fmt.Sprintf("https://bench.weblimb.ai/docs/%07d", docID)
	title := fmt.Sprintf("Benchmark Document #%07d", docID)

	return SyntheticDoc{
		DocID:         docID,
		URL:           url,
		Title:         title,
		CleanBody:     title,
		TermPositions: termPositions,
		TotalTokens:   docLen,
		Vector:        vec,
	}
}

// QueryType defines the frequency tier for query sampling.
type QueryType int

const (
	HeadQuery QueryType = iota
	TorsoQuery
	TailQuery
)

// GenerateQueryPool pre-generates a fixed pool of multi-term queries sampled by Zipfian tiers.
func (zg *ZipfianGenerator) GenerateQueryPool(numQueries int, seed int64) []string {
	rng := mrand.New(mrand.NewSource(seed))
	queries := make([]string, numQueries)

	for i := 0; i < numQueries; i++ {
		// 60% 2-term, 30% 3-term, 10% 1-term
		numTerms := 2
		lenRoll := rng.Float64()
		if lenRoll < 0.10 {
			numTerms = 1
		} else if lenRoll > 0.70 {
			numTerms = 3
		}

		// 30% Head (Rank 1-100), 50% Torso (Rank 101-5000), 20% Tail (Rank 5001-50000)
		tierRoll := rng.Float64()
		var minRank, maxRank int
		if tierRoll < 0.30 {
			minRank, maxRank = 0, 100
		} else if tierRoll < 0.80 {
			minRank, maxRank = 100, 5000
		} else {
			minRank, maxRank = 5000, VocabSize
		}

		terms := make([]string, numTerms)
		for t := 0; t < numTerms; t++ {
			rank := minRank + rng.Intn(maxRank-minRank)
			terms[t] = zg.termDict[rank]
		}

		queryStr := terms[0]
		for t := 1; t < numTerms; t++ {
			queryStr += " " + terms[t]
		}
		queries[i] = queryStr
	}

	return queries
}
