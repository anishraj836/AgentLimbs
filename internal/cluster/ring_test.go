package cluster

import (
	"fmt"
	"math"
	"testing"
)

func TestHashRing_EmptyRing(t *testing.T) {
	ring := NewHashRing(128)
	node, err := ring.GetNode("doc-123")
	if err != ErrEmptyRing {
		t.Fatalf("expected ErrEmptyRing, got: %v (node=%s)", err, node)
	}

	nodes, err := ring.GetNodes("doc-123", 3)
	if err != ErrEmptyRing {
		t.Fatalf("expected ErrEmptyRing for GetNodes, got: %v (nodes=%v)", err, nodes)
	}
}

func TestHashRing_AddAndRemoveNode(t *testing.T) {
	ring := NewHashRing(128)
	ring.AddNode("node-1")
	ring.AddNode("node-2")
	ring.AddNode("node-3")

	// Idempotent add
	ring.AddNode("node-1")

	nodes := ring.Nodes()
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes, got: %d (%v)", len(nodes), nodes)
	}

	// Key lookup
	node, err := ring.GetNode("https://example.com/guide")
	if err != nil {
		t.Fatalf("GetNode failed: %v", err)
	}
	if node != "node-1" && node != "node-2" && node != "node-3" {
		t.Fatalf("unexpected node returned: %s", node)
	}

	// Multi-replica lookup
	replicas, err := ring.GetNodes("https://example.com/guide", 2)
	if err != nil {
		t.Fatalf("GetNodes failed: %v", err)
	}
	if len(replicas) != 2 {
		t.Fatalf("expected 2 replicas, got %d (%v)", len(replicas), replicas)
	}
	if replicas[0] == replicas[1] {
		t.Fatalf("expected distinct replica nodes, got duplicates: %v", replicas)
	}

	// Remove node
	ring.RemoveNode("node-2")
	nodes = ring.Nodes()
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes after removal, got: %d (%v)", len(nodes), nodes)
	}

	// Verify removed node is not returned
	for i := 0; i < 100; i++ {
		n, err := ring.GetNode(fmt.Sprintf("key-%d", i))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if n == "node-2" {
			t.Fatalf("removed node 'node-2' was returned for key-%d", i)
		}
	}
}

func TestHashRing_Distribution(t *testing.T) {
	ring := NewHashRing(128)
	nodeCount := 5
	for i := 1; i <= nodeCount; i++ {
		ring.AddNode(fmt.Sprintf("node-%d", i))
	}

	totalKeys := 10000
	counts := make(map[string]int)

	for i := 0; i < totalKeys; i++ {
		key := fmt.Sprintf("https://domain.com/page/%d", i)
		node, err := ring.GetNode(key)
		if err != nil {
			t.Fatalf("GetNode failed: %v", err)
		}
		counts[node]++
	}

	expectedPerNode := float64(totalKeys) / float64(nodeCount)
	var sumSqDiff float64
	for _, c := range counts {
		diff := float64(c) - expectedPerNode
		sumSqDiff += diff * diff
	}
	stdDev := math.Sqrt(sumSqDiff / float64(nodeCount))
	relStdDev := stdDev / expectedPerNode

	t.Logf("Distribution over %d keys: %v (stdDev=%.2f, relStdDev=%.2f%%)", totalKeys, counts, stdDev, relStdDev*100)

	// Standard deviation should be under 20% across 5 nodes
	if relStdDev > 0.20 {
		t.Errorf("relative standard deviation too high: %.2f%% (expected < 20%%)", relStdDev*100)
	}
}

func TestGetPartition(t *testing.T) {
	p1 := GetPartition("https://golang.org/doc", 16)
	p2 := GetPartition("https://golang.org/doc", 16)
	if p1 != p2 {
		t.Fatalf("expected deterministic partition, got %d vs %d", p1, p2)
	}
	if p1 < 0 || p1 >= 16 {
		t.Fatalf("partition %d out of bounds [0, 15]", p1)
	}
}
