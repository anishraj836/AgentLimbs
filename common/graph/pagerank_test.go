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
}
