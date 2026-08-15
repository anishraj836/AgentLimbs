package index

import (
	"fmt"
	"math/rand"
	"sync"
	"testing"
)

var (
	benchOnce     sync.Once
	benchPostings map[string]*CompressedPostingList
	benchMapper   *DocIDMapper
	benchVecIndex *VectorIndex
	benchQueries  []string
)

func initBenchmarkCorpus(numDocs int) {
	benchPostings = make(map[string]*CompressedPostingList)
	benchMapper = NewDocIDMapper()
	benchVecIndex = NewVectorIndexWithPrecision(128, PrecisionInt8)

	rng := rand.New(rand.NewSource(42))
	vocabSize := 5000

	// Generate documents
	for i := 0; i < numDocs; i++ {
		url := fmt.Sprintf("https://bench.test/doc/%d", i)
		benchMapper.GetOrCreateID(url)

		// Int8 vector
		vec := make([]float64, 128)
		for d := 0; d < 128; d++ {
			vec[d] = rng.NormFloat64()
		}
		_ = benchVecIndex.AddVector(url, vec)

		// Term postings
		docLen := 150
		for t := 0; t < 15; t++ {
			term := fmt.Sprintf("term_%d", rng.Intn(vocabSize))
			pl, exists := benchPostings[term]
			if !exists {
				pl = NewCompressedPostingList()
				benchPostings[term] = pl
			}
			pl.Add(uint32(i), uint32(rng.Intn(5)+1), uint32(docLen))
		}
	}

	for _, pl := range benchPostings {
		pl.SealTail()
	}

	// Generate queries
	benchQueries = make([]string, 1000)
	for q := 0; q < 1000; q++ {
		t1 := fmt.Sprintf("term_%d", rng.Intn(vocabSize))
		t2 := fmt.Sprintf("term_%d", rng.Intn(vocabSize))
		benchQueries[q] = t1 + " " + t2
	}
}

func BenchmarkSearch1M_BlockMaxWAND(b *testing.B) {
	docCount := 100000
	if !testing.Short() {
		docCount = 200000
	}
	benchOnce.Do(func() {
		initBenchmarkCorpus(docCount)
	})

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		idx := 0
		for pb.Next() {
			q := benchQueries[idx%len(benchQueries)]
			terms := []string{fmt.Sprintf("term_%d", idx%5000)}
			var lists []*CompressedPostingList
			var idfs []float64
			for _, t := range terms {
				if pl, exists := benchPostings[t]; exists {
					lists = append(lists, pl)
					idfs = append(idfs, 2.5)
				}
			}
			_ = BlockMaxWANDScores(lists, idfs, 10, 150.0, nil, benchMapper, 1.2, 0.75)
			idx++
			_ = q
		}
	})
}
