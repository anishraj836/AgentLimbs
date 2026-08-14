package graph

import (
	"testing"
)

func TestPageRank(t *testing.T) {
	g := NewWebGraph()

	// Link structure: Site A and Site B link to authoritative Site C
	g.AddLink("https://siteA.com", "https://siteC.com")
	g.AddLink("https://siteB.com", "https://siteC.com")

	ranks := g.ComputePageRank(0.85, 20)

	if len(ranks) == 0 {
		t.Fatalf("expected PageRank scores, got none")
	}

	if ranks["https://siteC.com"] <= ranks["https://siteA.com"] {
		t.Errorf("expected siteC to have higher PageRank than siteA due to incoming links, got ranks: %v", ranks)
	}

	var sum float64
	for _, r := range ranks {
		sum += r
	}
	if sum < 0.999999 || sum > 1.000001 {
		t.Errorf("expected sum of PageRank scores to equal 1.0 (within 1e-6), got %f", sum)
	}
}

func TestPageRankMassConservationWithDanglingNodes(t *testing.T) {
	// Test graph with multiple dangling sink nodes and disconnected components
	g := NewWebGraph()

	// A -> B -> C (C is dangling)
	// A -> D (D is dangling)
	// E -> F (F is dangling)
	g.AddLink("https://siteA.com", "https://siteB.com")
	g.AddLink("https://siteB.com", "https://siteC.com")
	g.AddLink("https://siteA.com", "https://siteD.com")
	g.AddLink("https://siteE.com", "https://siteF.com")

	dampingFactors := []float64{0.5, 0.7, 0.85, 0.95}
	iterationsList := []int{1, 5, 20, 50, 100}

	for _, df := range dampingFactors {
		for _, iters := range iterationsList {
			ranks := g.ComputePageRank(df, iters)
			if len(ranks) != 6 {
				t.Fatalf("expected 6 nodes in ranks, got %d", len(ranks))
			}

			var sum float64
			for _, score := range ranks {
				sum += score
			}

			epsilon := 1e-6
			diff := sum - 1.0
			if diff < 0 {
				diff = -diff
			}
			if diff > epsilon {
				t.Errorf("PageRank probability mass not conserved for df=%f, iters=%d: sum=%f, diff=%e", df, iters, sum, diff)
			}
		}
	}
}

func TestPageRankEmptyGraph(t *testing.T) {
	g := NewWebGraph()
	ranks := g.ComputePageRank(0.85, 20)
	if len(ranks) != 0 {
		t.Errorf("expected empty ranks map for empty graph, got %v", ranks)
	}
}

