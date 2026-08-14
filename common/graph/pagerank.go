package graph

import (
	"sync"
)

// WebGraph stores directed links between URLs for PageRank authority calculation.
type WebGraph struct {
	mu       sync.RWMutex
	outLinks map[string]map[string]bool // Source -> Set of Destinations
	inLinks  map[string]map[string]bool // Destination -> Set of Sources
	nodes    map[string]bool
}

func NewWebGraph() *WebGraph {
	return &WebGraph{
		outLinks: make(map[string]map[string]bool),
		inLinks:  make(map[string]map[string]bool),
		nodes:    make(map[string]bool),
	}
}

// AddLink inserts a directed edge from srcURL to dstURL.
func (g *WebGraph) AddLink(srcURL, dstURL string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.nodes[srcURL] = true
	g.nodes[dstURL] = true

	if g.outLinks[srcURL] == nil {
		g.outLinks[srcURL] = make(map[string]bool)
	}
	g.outLinks[srcURL][dstURL] = true

	if g.inLinks[dstURL] == nil {
		g.inLinks[dstURL] = make(map[string]bool)
	}
	g.inLinks[dstURL][srcURL] = true
}

// ComputePageRank calculates Google PageRank scores using power iteration.
// PR(A) = (1-d)/N + d * (sum(PR(T_i) / OutLinks(T_i)) + DanglingSum / N)
func (g *WebGraph) ComputePageRank(dampingFactor float64, iterations int) map[string]float64 {
	g.mu.RLock()
	defer g.mu.RUnlock()

	N := len(g.nodes)
	if N == 0 {
		return map[string]float64{}
	}

	ranks := make(map[string]float64)
	initialRank := 1.0 / float64(N)

	for node := range g.nodes {
		ranks[node] = initialRank
	}

	for it := 0; it < iterations; it++ {
		var danglingSum float64
		for node := range g.nodes {
			if len(g.outLinks[node]) == 0 {
				danglingSum += ranks[node]
			}
		}

		newRanks := make(map[string]float64)

		for node := range g.nodes {
			var incomingSum float64
			for inNode := range g.inLinks[node] {
				outCount := len(g.outLinks[inNode])
				if outCount > 0 {
					incomingSum += ranks[inNode] / float64(outCount)
				}
			}

			newRanks[node] = ((1.0 - dampingFactor) / float64(N)) + (dampingFactor * (incomingSum + (danglingSum / float64(N))))
		}

		ranks = newRanks
	}

	return ranks
}
