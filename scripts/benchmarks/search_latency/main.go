package main

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/crawler-monorepo/common/stemmer"
	"github.com/crawler-monorepo/common/stopwords"
	"github.com/crawler-monorepo/internal/index"
	"github.com/crawler-monorepo/internal/search"
)

func main() {
	engine := index.NewEngine()

	fmt.Println("=========================================================================")
	fmt.Println("           AGENTLIMBS HYBRID SEARCH LATENCY BENCHMARK                    ")
	fmt.Println("=========================================================================")
	fmt.Println("Hydrating in-memory index with 1,000 synthetic technical documents...")

	topics := []string{"golang", "concurrency", "vector", "database", "security", "search", "crawler", "memory", "raft", "kafka"}

	for i := 1; i <= 1000; i++ {
		t1 := topics[rand.Intn(len(topics))]
		t2 := topics[rand.Intn(len(topics))]
		url := fmt.Sprintf("https://docs.example.com/item-%d", i)
		title := fmt.Sprintf("Technical Guide to %s and %s System Architecture #%d", t1, t2, i)
		body := fmt.Sprintf("This document covers deep engineering topics regarding %s performance optimization, %s memory bounds, lock safety, and distributed consensus scaling.", t1, t2)

		rawTokens := strings.Fields(strings.ToLower(body))
		termPositions := make(map[string][]int)
		for idx, raw := range rawTokens {
			clean := strings.Trim(raw, ".,!?:;\"'()[]{}")
			if clean == "" || stopwords.IsStopword(clean) {
				continue
			}
			stemmed := stemmer.Stem(clean)
			termPositions[stemmed] = append(termPositions[stemmed], idx)
		}

		engine.IndexDocument(url, title, body, termPositions, len(rawTokens))
	}

	titles, urls, bodies := engine.GetMetadataMaps()

	const numWorkers = 10
	const queriesPerWorker = 1000
	totalQueries := numWorkers * queriesPerWorker

	durations := make([]time.Duration, totalQueries)
	var wg sync.WaitGroup
	wg.Add(numWorkers)

	startTime := time.Now()

	for w := 0; w < numWorkers; w++ {
		workerID := w
		go func() {
			defer wg.Done()
			for q := 0; q < queriesPerWorker; q++ {
				qTopic := topics[rand.Intn(len(topics))]

				t0 := time.Now()
				bm25Hits := engine.Inverted.RankDocuments(qTopic, titles, urls, bodies, 10)
				vectorHits := engine.SearchVector(qTopic, 10)
				_ = search.ReciprocalRankFusion(qTopic, bm25Hits, vectorHits, 5, titles, urls, bodies)
				elapsed := time.Since(t0)

				durations[workerID*queriesPerWorker+q] = elapsed
			}
		}()
	}

	wg.Wait()
	totalElapsed := time.Since(startTime)

	sort.Slice(durations, func(i, j int) bool {
		return durations[i] < durations[j]
	})

	p50 := durations[int(float64(totalQueries)*0.50)]
	p90 := durations[int(float64(totalQueries)*0.90)]
	p95 := durations[int(float64(totalQueries)*0.95)]
	p99 := durations[int(float64(totalQueries)*0.99)]

	var sum time.Duration
	for _, d := range durations {
		sum += d
	}
	avg := sum / time.Duration(totalQueries)
	qps := float64(totalQueries) / totalElapsed.Seconds()

	fmt.Printf("\nExecuted %d hybrid BM25 + Vector + Trie RRF searches across %d concurrent workers:\n", totalQueries, numWorkers)
	fmt.Printf("  • Average Latency:  %v (%.3f ms)\n", avg, float64(avg.Microseconds())/1000.0)
	fmt.Printf("  • p50 (Median):     %v (%.3f ms)\n", p50, float64(p50.Microseconds())/1000.0)
	fmt.Printf("  • p90 Latency:      %v (%.3f ms)\n", p90, float64(p90.Microseconds())/1000.0)
	fmt.Printf("  • p95 Latency:      %v (%.3f ms)\n", p95, float64(p95.Microseconds())/1000.0)
	fmt.Printf("  • p99 Latency:      %v (%.3f ms)\n", p99, float64(p99.Microseconds())/1000.0)
	fmt.Printf("  • Throughput:       %.2f Queries/Sec (QPS)\n", qps)
	fmt.Println("=========================================================================")
}
