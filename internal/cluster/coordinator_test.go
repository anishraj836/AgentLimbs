package cluster

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/crawler-monorepo/internal/index"
	"github.com/crawler-monorepo/internal/search"
)

// MockShardSearchClient simulates remote shard nodes.
type MockShardSearchClient struct {
	shards map[string]*index.Engine
	failed map[string]bool
}

func (m *MockShardSearchClient) SearchShard(ctx context.Context, peer string, query string, topK int) ([]search.HybridSearchHit, error) {
	if m.failed[peer] {
		return nil, errors.New("connection refused / timeout")
	}

	eng, ok := m.shards[peer]
	if !ok || eng == nil {
		return nil, fmt.Errorf("shard %s not found", peer)
	}

	titles, urls, bodies := eng.GetMetadataMaps()
	bm25Hits := eng.SearchBM25(query, topK*2)
	vecHits := eng.SearchVector(query, topK*2)

	hits := search.ReciprocalRankFusion(query, bm25Hits, vecHits, topK, titles, urls, bodies)
	return hits, nil
}

func TestClusterCoordinator_ScatterGatherSearch(t *testing.T) {
	// Setup 3 shards
	shard1Eng := index.NewEngine()
	shard1Eng.IndexDocumentDirectly("https://doc.com/k8s", "Kubernetes Orchestration", "Kubernetes container cluster scheduling pods.", 8, "https://doc.com/k8s")

	shard2Eng := index.NewEngine()
	shard2Eng.IndexDocumentDirectly("https://doc.com/docker", "Docker Containers", "Docker containerization runtime images engine.", 8, "https://doc.com/docker")

	shard3Eng := index.NewEngine()
	shard3Eng.IndexDocumentDirectly("https://doc.com/grpc", "gRPC Protobuf", "High performance RPC framework with HTTP2.", 8, "https://doc.com/grpc")

	mockClient := &MockShardSearchClient{
		shards: map[string]*index.Engine{
			"shard-1": shard1Eng,
			"shard-2": shard2Eng,
			"shard-3": shard3Eng,
		},
		failed: make(map[string]bool),
	}

	ring := NewHashRing(128)
	ring.AddNode("shard-1")
	ring.AddNode("shard-2")
	ring.AddNode("shard-3")

	coord := NewClusterCoordinator("shard-1", ring, nil, shard1Eng, mockClient, 16)

	// 1. Healthy Scatter-Gather Search
	resp, err := coord.ScatterGatherSearch(context.Background(), "container cluster", 5)
	if err != nil {
		t.Fatalf("ScatterGatherSearch failed: %v", err)
	}

	if resp.Degraded {
		t.Errorf("expected healthy search, got degraded=true")
	}
	if resp.ShardsQueried != 3 || resp.ShardsResponded != 3 {
		t.Errorf("expected 3/3 shards responded, got %d/%d", resp.ShardsResponded, resp.ShardsQueried)
	}
	if len(resp.Results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(resp.Results))
	}

	// 2. Partial Failure Resilience (1 Shard down)
	mockClient.failed["shard-2"] = true

	respDegraded, err := coord.ScatterGatherSearch(context.Background(), "container cluster", 5)
	if err != nil {
		t.Fatalf("ScatterGatherSearch should not fail when 1 shard fails: %v", err)
	}

	if !respDegraded.Degraded {
		t.Errorf("expected degraded=true when shard-2 failed")
	}
	if respDegraded.ShardsQueried != 3 || respDegraded.ShardsResponded != 2 {
		t.Errorf("expected 2/3 shards responded, got %d/%d", respDegraded.ShardsResponded, respDegraded.ShardsQueried)
	}
	if len(respDegraded.Results) == 0 {
		t.Errorf("expected results from healthy shards, got 0")
	}
}
