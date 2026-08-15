package main

import (
	"encoding/json"
	"fmt"
	"math"
	"runtime"
	"sort"
	"time"
)

// MemoryProfile captures granular heap and system memory statistics.
type MemoryProfile struct {
	HeapAllocMB          float64 `json:"heap_alloc_mb"`
	HeapInuseMB          float64 `json:"heap_inuse_mb"`
	SysMB                float64 `json:"sys_mb"`
	LiveHeapBytesPerDoc  float64 `json:"live_heap_bytes_per_doc"`
	HeapInuseBytesPerDoc float64 `json:"heap_inuse_bytes_per_doc"`
	RAMSavingsPercent    float64 `json:"ram_savings_percent"`
	NumGC                uint32  `json:"num_gc"`
	GCPauseTotalMs       float64 `json:"gc_pause_total_ms"`
}

// LatencyPercentiles holds sorted query latency thresholds in milliseconds.
type LatencyPercentiles struct {
	P50  float64 `json:"p50_ms"`
	P90  float64 `json:"p90_ms"`
	P99  float64 `json:"p99_ms"`
	P999 float64 `json:"p999_ms"`
	Min  float64 `json:"min_ms"`
	Max  float64 `json:"max_ms"`
	Mean float64 `json:"mean_ms"`
}

// BenchmarkReport captures the comprehensive benchmark telemetry for Stage 7.
type BenchmarkReport struct {
	Benchmark                 string             `json:"benchmark"`
	DocumentsIndexed          int                `json:"documents_indexed"`
	VectorDimension           int                `json:"vector_dimension"`
	Precision                 string             `json:"precision"`
	Compression               string             `json:"compression"`
	IngestionTimeSec          float64            `json:"ingestion_time_sec"`
	IngestionThroughputDocsSec float64           `json:"ingestion_throughput_docs_sec"`
	Memory                    MemoryProfile      `json:"memory"`
	BM25Search                LatencyPercentiles `json:"bm25_search_latencies"`
	TwoStageSearch            LatencyPercentiles `json:"two_stage_hybrid_latencies"`
	TotalQueries              int                `json:"total_queries"`
	SearchThroughputQPS       float64            `json:"search_throughput_qps"`
}

// ForceGCAndSample executes a 2-phase GC stabilization sweep before reading memory stats.
func ForceGCAndSample() runtime.MemStats {
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m
}

// ComputeMemoryProfile computes net memory consumption and per-document metrics.
func ComputeMemoryProfile(m0, m1 runtime.MemStats, numDocs int, dim int) MemoryProfile {
	heapAllocDelta := int64(m1.HeapAlloc) - int64(m0.HeapAlloc)
	if heapAllocDelta < 0 {
		heapAllocDelta = 0
	}
	heapInuseDelta := int64(m1.HeapInuse) - int64(m0.HeapInuse)
	if heapInuseDelta < 0 {
		heapInuseDelta = 0
	}

	liveBytesPerDoc := float64(heapAllocDelta) / float64(numDocs)
	inuseBytesPerDoc := float64(heapInuseDelta) / float64(numDocs)

	// Baseline uncompressed float64 + raw inverted index estimation
	// Raw float64 vector = dim * 8 bytes + 24 slice header + map entry (~100B)
	// Raw posting list uncompressed = ~800 bytes/doc
	baselineBytesPerDoc := float64(dim*8+124) + 800.0
	savingsPct := (1.0 - (liveBytesPerDoc / baselineBytesPerDoc)) * 100.0
	if savingsPct < 0 {
		savingsPct = 0
	}

	return MemoryProfile{
		HeapAllocMB:          float64(m1.HeapAlloc) / (1024 * 1024),
		HeapInuseMB:          float64(m1.HeapInuse) / (1024 * 1024),
		SysMB:                float64(m1.Sys) / (1024 * 1024),
		LiveHeapBytesPerDoc:  math.Round(liveBytesPerDoc*100) / 100,
		HeapInuseBytesPerDoc: math.Round(inuseBytesPerDoc*100) / 100,
		RAMSavingsPercent:    math.Round(savingsPct*100) / 100,
		NumGC:                m1.NumGC - m0.NumGC,
		GCPauseTotalMs:       float64(m1.PauseTotalNs-m0.PauseTotalNs) / 1e6,
	}
}

// ComputePercentiles merges thread-local durations and calculates exact percentiles.
func ComputePercentiles(durations []time.Duration) LatencyPercentiles {
	n := len(durations)
	if n == 0 {
		return LatencyPercentiles{}
	}

	sort.Slice(durations, func(i, j int) bool {
		return durations[i] < durations[j]
	})

	toMs := func(d time.Duration) float64 {
		return float64(d.Microseconds()) / 1000.0
	}

	getPercentile := func(p float64) float64 {
		idx := int(math.Floor((p / 100.0) * float64(n)))
		if idx < 0 {
			idx = 0
		}
		if idx >= n {
			idx = n - 1
		}
		return toMs(durations[idx])
	}

	totalMs := 0.0
	for _, d := range durations {
		totalMs += toMs(d)
	}

	return LatencyPercentiles{
		P50:  getPercentile(50.0),
		P90:  getPercentile(90.0),
		P99:  getPercentile(99.0),
		P999: getPercentile(99.9),
		Min:  toMs(durations[0]),
		Max:  toMs(durations[n-1]),
		Mean: math.Round((totalMs/float64(n))*1000) / 1000,
	}
}

// PrintReport outputs the formatted benchmark summary.
func (r *BenchmarkReport) PrintReport(asJSON bool) {
	if asJSON {
		data, _ := json.MarshalIndent(r, "", "  ")
		fmt.Println(string(data))
		return
	}

	fmt.Println("\n================================================================================")
	fmt.Printf("🚀 WebLimbAI 1,000,000-Document Multi-Core Benchmark Report (%s)\n", r.Benchmark)
	fmt.Println("================================================================================")
	fmt.Printf("  • Total Documents Indexed  : %d docs\n", r.DocumentsIndexed)
	fmt.Printf("  • Vector Dimension         : %d-D\n", r.VectorDimension)
	fmt.Printf("  • Quantization Mode        : %s\n", r.Precision)
	fmt.Printf("  • Posting Compression      : %s (Block Size B=64)\n", r.Compression)
	fmt.Printf("  • Total Ingestion Duration : %.2f sec\n", r.IngestionTimeSec)
	fmt.Printf("  • Ingestion Throughput     : %.1f docs/sec\n", r.IngestionThroughputDocsSec)
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Println("📊 Memory Footprint Analysis:")
	fmt.Printf("  • Post-GC Heap Alloc       : %.2f MB\n", r.Memory.HeapAllocMB)
	fmt.Printf("  • Post-GC Heap Inuse       : %.2f MB\n", r.Memory.HeapInuseMB)
	fmt.Printf("  • System Memory (Sys)      : %.2f MB\n", r.Memory.SysMB)
	fmt.Printf("  • Live Heap Per Document   : %.2f bytes/doc\n", r.Memory.LiveHeapBytesPerDoc)
	fmt.Printf("  • Heap Inuse Per Document  : %.2f bytes/doc\n", r.Memory.HeapInuseBytesPerDoc)
	fmt.Printf("  • Net RAM Savings vs F64   : %.2f%%\n", r.Memory.RAMSavingsPercent)
	fmt.Printf("  • GC Cycles Occurred       : %d cycles (Total Pause: %.2f ms)\n", r.Memory.NumGC, r.Memory.GCPauseTotalMs)
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Println("⚡ Query Latency Breakdown:")
	fmt.Printf("  • Pure Block-Max WAND BM25 : P50=%.3fms | P90=%.3fms | P99=%.3fms | P99.9=%.3fms\n",
		r.BM25Search.P50, r.BM25Search.P90, r.BM25Search.P99, r.BM25Search.P999)
	fmt.Printf("  • Two-Stage Hybrid (BM25+V): P50=%.3fms | P90=%.3fms | P99=%.3fms | P99.9=%.3fms\n",
		r.TwoStageSearch.P50, r.TwoStageSearch.P90, r.TwoStageSearch.P99, r.TwoStageSearch.P999)
	fmt.Printf("  • Search Throughput        : %.1f QPS (across %d queries)\n", r.SearchThroughputQPS, r.TotalQueries)
	fmt.Println("================================================================================")
}
