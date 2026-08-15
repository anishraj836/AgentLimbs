package cluster

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/crawler-monorepo/internal/index"
	"github.com/crawler-monorepo/internal/search"
)

// SearchShardRequest represents the query sent to an individual shard node.
type SearchShardRequest struct {
	Query string `json:"query"`
	TopK  int    `json:"top_k"`
}

// SearchShardResponse represents candidate hits returned by an individual shard.
type SearchShardResponse struct {
	ShardID   string                   `json:"shard_id"`
	Hits      []search.HybridSearchHit `json:"hits"`
	LatencyMs float64                  `json:"latency_ms"`
	Error     string                   `json:"error,omitempty"`
}

// ClusterSearchResponse encapsulates the global reduced search result with cluster telemetry.
type ClusterSearchResponse struct {
	Query           string                   `json:"query"`
	LatencyMs       float64                  `json:"latency_ms"`
	TotalHits       int                      `json:"total_hits"`
	Results         []search.HybridSearchHit `json:"results"`
	Degraded        bool                     `json:"degraded,omitempty"`
	ShardsQueried   int                      `json:"shards_queried"`
	ShardsResponded int                      `json:"shards_responded"`
}

// ShardSearchClient abstracts remote shard query execution for scatter-gather searches.
type ShardSearchClient interface {
	SearchShard(ctx context.Context, peer string, query string, topK int) ([]search.HybridSearchHit, error)
}

// ClusterCoordinator orchestrates partitioned index dispatching, Raft proposals, and scatter-gather queries.
type ClusterCoordinator struct {
	mu          sync.RWMutex
	nodeID      string
	ring        *HashRing
	raftNode    *RaftNode
	engine      *index.Engine
	transport   ShardSearchClient
	totalShards int
}

// NewClusterCoordinator creates a new coordinator instance.
func NewClusterCoordinator(nodeID string, ring *HashRing, raftNode *RaftNode, engine *index.Engine, transport ShardSearchClient, totalShards int) *ClusterCoordinator {
	if totalShards <= 0 {
		totalShards = 16
	}
	if engine == nil {
		engine = index.GlobalEngine
	}
	return &ClusterCoordinator{
		nodeID:      nodeID,
		ring:        ring,
		raftNode:    raftNode,
		engine:      engine,
		transport:   transport,
		totalShards: totalShards,
	}
}

// NodeID returns this coordinator's physical node identifier.
func (c *ClusterCoordinator) NodeID() string {
	return c.nodeID
}

// ExecuteLocalShardSearch executes hybrid BM25 + Vector search on local engine and returns hydrated hits.
func (c *ClusterCoordinator) ExecuteLocalShardSearch(query string, topK int) ([]search.HybridSearchHit, error) {
	if topK <= 0 {
		topK = 10
	}

	bm25Hits := c.engine.SearchBM25(query, topK*2)
	vecHits := c.engine.SearchVector(query, topK*2)

	titles, urls, bodies := c.engine.GetMetadataMaps()

	// Perform Reciprocal Rank Fusion on local shard candidates
	hits := search.ReciprocalRankFusion(query, bm25Hits, vecHits, topK, titles, urls, bodies)
	return hits, nil
}

// ScatterGatherSearch fans out search requests across all cluster nodes in parallel without error cascade.
func (c *ClusterCoordinator) ScatterGatherSearch(ctx context.Context, query string, topK int) (*ClusterSearchResponse, error) {
	startTime := time.Now()
	if topK <= 0 {
		topK = 10
	}

	nodes := c.ring.Nodes()

	// If single node or local-only
	if len(nodes) <= 1 || c.transport == nil {
		localHits, err := c.ExecuteLocalShardSearch(query, topK)
		if err != nil {
			return nil, err
		}
		return &ClusterSearchResponse{
			Query:           query,
			LatencyMs:       float64(time.Since(startTime).Microseconds()) / 1000.0,
			TotalHits:       len(localHits),
			Results:         localHits,
			Degraded:        false,
			ShardsQueried:   1,
			ShardsResponded: 1,
		}, nil
	}

	type shardResult struct {
		nodeID string
		hits   []search.HybridSearchHit
		err    error
	}

	resultsChan := make(chan shardResult, len(nodes))
	var wg sync.WaitGroup

	for _, n := range nodes {
		wg.Add(1)
		go func(targetNode string) {
			defer wg.Done()

			if targetNode == c.nodeID {
				hits, err := c.ExecuteLocalShardSearch(query, topK)
				resultsChan <- shardResult{nodeID: targetNode, hits: hits, err: err}
				return
			}

			shardCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
			defer cancel()

			hits, err := c.transport.SearchShard(shardCtx, targetNode, query, topK)
			resultsChan <- shardResult{nodeID: targetNode, hits: hits, err: err}
		}(n)
	}

	wg.Wait()
	close(resultsChan)

	shardsQueried := len(nodes)
	shardsResponded := 0
	candidatesMap := make(map[string]search.HybridSearchHit)

	for res := range resultsChan {
		if res.err == nil {
			shardsResponded++
			for _, hit := range res.hits {
				key := hit.URL
				if key == "" {
					key = hit.DocID
				}
				if existing, exists := candidatesMap[key]; !exists || hit.RRFScore > existing.RRFScore {
					candidatesMap[key] = hit
				}
			}
		}
	}

	candidates := make([]search.HybridSearchHit, 0, len(candidatesMap))
	for _, hit := range candidatesMap {
		candidates = append(candidates, hit)
	}

	// Deterministic sort with float tolerance and DocID tie-breaker
	sort.Slice(candidates, func(i, j int) bool {
		if math.Abs(candidates[i].RRFScore-candidates[j].RRFScore) < 1e-9 {
			return candidates[i].DocID < candidates[j].DocID
		}
		return candidates[i].RRFScore > candidates[j].RRFScore
	})

	if len(candidates) > topK {
		candidates = candidates[:topK]
	}

	degraded := shardsResponded < shardsQueried

	return &ClusterSearchResponse{
		Query:           query,
		LatencyMs:       float64(time.Since(startTime).Microseconds()) / 1000.0,
		TotalHits:       len(candidates),
		Results:         candidates,
		Degraded:        degraded,
		ShardsQueried:   shardsQueried,
		ShardsResponded: shardsResponded,
	}, nil
}

// ProposeIndexDocument submits an index request to the cluster via Raft or direct execution.
func (c *ClusterCoordinator) ProposeIndexDocument(ctx context.Context, payload IndexDocPayload) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	if c.raftNode != nil {
		_, _, err := c.raftNode.Propose(ctx, CmdIndexDocument, data)
		return err
	}

	// Direct local indexing fallback
	c.engine.IndexDocumentWithSource(payload.URL, payload.Title, payload.CleanBody, nil, payload.TotalTokens, "cluster", payload.SourceURL)
	return nil
}

// ProposeDeleteDocument submits a delete request to the cluster via Raft or direct execution.
func (c *ClusterCoordinator) ProposeDeleteDocument(ctx context.Context, docURL string) error {
	payload := DeleteDocPayload{URL: docURL}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	if c.raftNode != nil {
		_, _, err := c.raftNode.Propose(ctx, CmdDeleteDocument, data)
		return err
	}

	c.engine.DeleteDocument(docURL)
	return nil
}

// ProposeSetPrecision submits a vector precision update request to the cluster via Raft.
func (c *ClusterCoordinator) ProposeSetPrecision(ctx context.Context, precision string) error {
	payload := SetPrecisionPayload{Precision: precision}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	if c.raftNode != nil {
		_, _, err := c.raftNode.Propose(ctx, CmdSetPrecision, data)
		return err
	}

	if vecIdx := c.engine.GetVectorIndex(); vecIdx != nil {
		return vecIdx.SetPrecision(index.VectorPrecision(precision))
	}
	return nil
}
