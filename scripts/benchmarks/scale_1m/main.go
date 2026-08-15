package main

import (
	"flag"
	"fmt"
	"math"
	mrand "math/rand"
	"os"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/crawler-monorepo/internal/index"
	"github.com/crawler-monorepo/internal/search"
)

func main() {
	numDocs := flag.Int("docs", 1000000, "Number of documents to index")
	dim := flag.Int("dim", 128, "Vector dimension (e.g. 128 or 768)")
	numWorkers := flag.Int("workers", runtime.GOMAXPROCS(0), "Number of concurrent worker goroutines")
	numQueries := flag.Int("queries", 10000, "Number of queries to benchmark")
	asJSON := flag.Bool("json", false, "Output results in machine-readable JSON format")
	enableProfile := flag.Bool("profile", false, "Enable CPU and Heap pprof profiling (cpu.prof, mem.prof)")
	flag.Parse()

	if *numWorkers <= 0 {
		*numWorkers = runtime.GOMAXPROCS(0)
	}

	if *enableProfile {
		cpuF, err := os.Create("cpu.prof")
		if err == nil {
			defer cpuF.Close()
			_ = pprof.StartCPUProfile(cpuF)
			defer pprof.StopCPUProfile()
		}
	}

	if !*asJSON {
		fmt.Println("================================================================================")
		fmt.Printf("⚡ Starting WebLimbAI 1M Document Benchmark (Workers=%d, Dim=%d, Docs=%d)\n", *numWorkers, *dim, *numDocs)
		fmt.Println("================================================================================")
	}

	gen := NewZipfianGenerator()

	// 1. Pre-GC baseline memory measurement
	m0 := ForceGCAndSample()

	// 2. Initialize quantized core indexes
	invIndex := index.NewInvertedIndex()
	docMapper := index.NewDocIDMapper()
	vecIndex := index.NewVectorIndexWithPrecision(*dim, index.PrecisionInt8)

	// Inverted posting lists map with mutex partitioning for high-concurrency ingestion
	const numShards = 64
	type postingShard struct {
		mu       sync.RWMutex
		postings map[string]*index.CompressedPostingList
	}
	shards := make([]postingShard, numShards)
	for s := 0; s < numShards; s++ {
		shards[s].postings = make(map[string]*index.CompressedPostingList)
	}

	getShard := func(term string) *postingShard {
		h := uint32(0)
		for i := 0; i < len(term); i++ {
			h = h*31 + uint32(term[i])
		}
		return &shards[h%numShards]
	}

	// 3. Multi-Core Ingestion Pipeline
	batchSize := 10000
	totalBatches := (*numDocs + batchSize - 1) / batchSize

	type DocBatch struct {
		BatchID  int
		StartID  uint32
		Count    int
		WorkerID int
	}

	batchChan := make(chan DocBatch, 4) // Bounded backpressure
	var globalDocIDCounter uint32
	var indexedDocsCounter uint64

	t0 := time.Now()

	// Progress reporting ticker
	stopProgress := make(chan struct{})
	if !*asJSON {
		go func() {
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-stopProgress:
					return
				case <-ticker.C:
					current := atomic.LoadUint64(&indexedDocsCounter)
					elapsed := time.Since(t0).Seconds()
					rate := float64(current) / elapsed
					var m runtime.MemStats
					runtime.ReadMemStats(&m)
					fmt.Printf("  ⏳ [%.1fs] Indexed: %d / %d docs (%.1f docs/sec) | Heap: %.1f MB\n",
						elapsed, current, *numDocs, rate, float64(m.HeapAlloc)/(1024*1024))
				}
			}
		}()
	}

	// Worker goroutines
	var wg sync.WaitGroup
	for w := 0; w < *numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			rng := mrand.New(mrand.NewSource(42 + int64(workerID)*10007))

			for batch := range batchChan {
				for i := 0; i < batch.Count; i++ {
					id := batch.StartID + uint32(i)
					doc := gen.GenerateDocument(id, *dim, rng)

					// 1. DocIDMapper
					docMapper.GetOrCreateID(doc.URL)

					// 2. Quantized Vector Index
					_ = vecIndex.AddVector(doc.URL, doc.Vector)

					// 3. Inverted Postings with Block-Max WAND tracking
					for term, positions := range doc.TermPositions {
						tf := uint32(len(positions))
						shard := getShard(term)
						shard.mu.Lock()
						pl, exists := shard.postings[term]
						if !exists {
							pl = index.NewCompressedPostingList()
							shard.postings[term] = pl
						}
						pl.Add(id, tf, uint32(doc.TotalTokens))
						shard.mu.Unlock()
					}
				}
				atomic.AddUint64(&indexedDocsCounter, uint64(batch.Count))
			}
		}(w)
	}

	// Producer dispatches batches
	for b := 0; b < totalBatches; b++ {
		count := batchSize
		remaining := *numDocs - int(globalDocIDCounter)
		if count > remaining {
			count = remaining
		}
		startID := atomic.AddUint32(&globalDocIDCounter, uint32(count)) - uint32(count)
		batchChan <- DocBatch{
			BatchID: b,
			StartID: startID,
			Count:   count,
		}
	}
	close(batchChan)
	wg.Wait()
	close(stopProgress)

	ingestionDuration := time.Since(t0).Seconds()
	ingestionThroughput := float64(*numDocs) / ingestionDuration

	// 4. Post-Ingestion Tail Sealing (SealTail pass)
	for s := 0; s < numShards; s++ {
		shards[s].mu.Lock()
		for _, pl := range shards[s].postings {
			pl.SealTail()
		}
		shards[s].mu.Unlock()
	}

	// 5. Post-Ingestion Memory Measurement
	m1 := ForceGCAndSample()
	memProfile := ComputeMemoryProfile(m0, m1, *numDocs, *dim)

	// 6. Pre-generate Zipfian query pool
	queryPool := gen.GenerateQueryPool(*numQueries, 1337)

	// Collect postings map for search
	allPostings := make(map[string]*index.CompressedPostingList)
	for s := 0; s < numShards; s++ {
		for term, pl := range shards[s].postings {
			allPostings[term] = pl
		}
	}

	// Flatten vector index data for fast 2-stage reranking
	avgDocLen := MeanDocLen
	totalDocsCount := int64(*numDocs)

	// 7. Concurrent Query Benchmark (Pure Block-Max WAND BM25)
	bm25Latencies := make([][]time.Duration, *numWorkers)
	for w := 0; w < *numWorkers; w++ {
		bm25Latencies[w] = make([]time.Duration, 0, *numQueries/ *numWorkers+10)
	}

	queriesPerWorker := (*numQueries + *numWorkers - 1) / *numWorkers
	tQueryStart := time.Now()

	var queryWg sync.WaitGroup
	for w := 0; w < *numWorkers; w++ {
		queryWg.Add(1)
		go func(workerID int) {
			defer queryWg.Done()
			startIdx := workerID * queriesPerWorker
			endIdx := startIdx + queriesPerWorker
			if endIdx > *numQueries {
				endIdx = *numQueries
			}

			for qIdx := startIdx; qIdx < endIdx; qIdx++ {
				qText := queryPool[qIdx]
				tQ0 := time.Now()

				// Block-Max WAND execution
				terms := strings.Fields(qText)
				var lists []*index.CompressedPostingList
				var idfs []float64
				for _, t := range terms {
					if pl, exists := allPostings[t]; exists {
						df := float64(pl.Count())
						if df > 0 {
							idf := math.Log(1.0 + (float64(totalDocsCount)-df+0.5)/(df+0.5))
							if idf > 0 {
								lists = append(lists, pl)
								idfs = append(idfs, idf)
							}
						}
					}
				}

				_ = index.BlockMaxWANDScores(
					lists,
					idfs,
					10,
					avgDocLen,
					nil,
					docMapper,
					1.2,
					0.75,
				)

				bm25Latencies[workerID] = append(bm25Latencies[workerID], time.Since(tQ0))
			}
		}(w)
	}
	queryWg.Wait()

	var mergedBM25Latencies []time.Duration
	for w := 0; w < *numWorkers; w++ {
		mergedBM25Latencies = append(mergedBM25Latencies, bm25Latencies[w]...)
	}
	bm25Percentiles := ComputePercentiles(mergedBM25Latencies)

	// 8. Concurrent Query Benchmark (Two-Stage Hybrid Search)
	twoStageLatencies := make([][]time.Duration, *numWorkers)
	for w := 0; w < *numWorkers; w++ {
		twoStageLatencies[w] = make([]time.Duration, 0, *numQueries/ *numWorkers+10)
	}

	rngQ := mrand.New(mrand.NewSource(999))
	dummyQueryVecs := make([][]float64, *numQueries)
	for i := 0; i < *numQueries; i++ {
		vec := make([]float64, *dim)
		for d := 0; d < *dim; d++ {
			vec[d] = rngQ.NormFloat64()
		}
		dummyQueryVecs[i] = vec
	}

	for w := 0; w < *numWorkers; w++ {
		queryWg.Add(1)
		go func(workerID int) {
			defer queryWg.Done()
			startIdx := workerID * queriesPerWorker
			endIdx := startIdx + queriesPerWorker
			if endIdx > *numQueries {
				endIdx = *numQueries
			}

			for qIdx := startIdx; qIdx < endIdx; qIdx++ {
				qText := queryPool[qIdx]
				qVec := dummyQueryVecs[qIdx]
				tQ0 := time.Now()

				// Stage 1: BM25 candidate retrieval (top 100)
				terms := strings.Fields(qText)
				var lists []*index.CompressedPostingList
				var idfs []float64
				for _, t := range terms {
					if pl, exists := allPostings[t]; exists {
						df := float64(pl.Count())
						if df > 0 {
							idf := math.Log(1.0 + (float64(totalDocsCount)-df+0.5)/(df+0.5))
							if idf > 0 {
								lists = append(lists, pl)
								idfs = append(idfs, idf)
							}
						}
					}
				}

				candidates := index.BlockMaxWANDScores(
					lists,
					idfs,
					100,
					avgDocLen,
					nil,
					docMapper,
					1.2,
					0.75,
				)

				// Stage 2: Int8 Vector rerank on candidates
				bm25Hits := make([]index.SearchHit, len(candidates))
				candidateURLs := make([]string, len(candidates))
				for i, c := range candidates {
					u, _ := docMapper.GetURL(c.DocID)
					bm25Hits[i] = index.SearchHit{DocID: u, Score: c.Score}
					candidateURLs[i] = u
				}

				vecHits := vecIndex.SearchSubset(qVec, candidateURLs, 10)

				_ = search.ReciprocalRankFusion(qText, bm25Hits, vecHits, 10)

				twoStageLatencies[workerID] = append(twoStageLatencies[workerID], time.Since(tQ0))
			}
		}(w)
	}
	queryWg.Wait()

	queryDuration := time.Since(tQueryStart).Seconds()
	qps := float64(*numQueries) / queryDuration

	var mergedTwoStageLatencies []time.Duration
	for w := 0; w < *numWorkers; w++ {
		mergedTwoStageLatencies = append(mergedTwoStageLatencies, twoStageLatencies[w]...)
	}
	twoStagePercentiles := ComputePercentiles(mergedTwoStageLatencies)

	if *enableProfile {
		memF, err := os.Create("mem.prof")
		if err == nil {
			defer memF.Close()
			runtime.GC()
			_ = pprof.WriteHeapProfile(memF)
		}
	}

	report := BenchmarkReport{
		Benchmark:                  "1M_quantized_vbyte_multi_core_stress_test",
		DocumentsIndexed:           *numDocs,
		VectorDimension:            *dim,
		Precision:                  "int8",
		Compression:                "vbyte_leb128_b64",
		IngestionTimeSec:           math.Round(ingestionDuration*100) / 100,
		IngestionThroughputDocsSec: math.Round(ingestionThroughput*10) / 10,
		Memory:                     memProfile,
		BM25Search:                 bm25Percentiles,
		TwoStageSearch:             twoStagePercentiles,
		TotalQueries:               *numQueries,
		SearchThroughputQPS:        math.Round(qps*10) / 10,
	}

	report.PrintReport(*asJSON)
	_ = invIndex
}
