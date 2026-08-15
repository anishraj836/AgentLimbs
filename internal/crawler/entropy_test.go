package crawler

import (
	"math"
	"testing"
)

func TestRuneShannonEntropy(t *testing.T) {
	// Empty anchor
	if s := RuneShannonEntropy(""); s != 0.05 {
		t.Errorf("expected 0.05 for empty anchor, got %f", s)
	}

	// Generic stop anchor
	if s := RuneShannonEntropy("click here"); s != 0.05 {
		t.Errorf("expected 0.05 for 'click here', got %f", s)
	}

	// Single descriptive technical word
	sTech := RuneShannonEntropy("Architecture")
	if sTech < 0.70 {
		t.Errorf("expected high score for 'Architecture', got %f", sTech)
	}

	// CJK Unicode string
	sCJK := RuneShannonEntropy("官方文档指南")
	if sCJK < 0.60 {
		t.Errorf("expected good score for CJK anchor, got %f", sCJK)
	}

	// Rich multi-word technical anchor
	sRich := RuneShannonEntropy("Distributed Raft Consensus Specification")
	if sRich < 0.80 {
		t.Errorf("expected high score for rich anchor, got %f", sRich)
	}
}

func TestPathSemanticScore(t *testing.T) {
	// Root URL
	sRoot := PathSemanticScore("https://go.dev/")
	if sRoot < 0.35 {
		t.Errorf("expected >= 0.35 for root, got %f", sRoot)
	}

	// High-value docs URL
	sDocs := PathSemanticScore("https://go.dev/doc/tutorial/getting-started.html")
	if sDocs < 0.80 {
		t.Errorf("expected high score for doc URL, got %f", sDocs)
	}

	// Deep non-tech path penalty
	sDeep := PathSemanticScore("https://example.com/a/b/c/d/e/f/g/item")
	if sDeep > 0.25 {
		t.Errorf("expected penalty for deep non-tech path, got %f", sDeep)
	}
}

func TestHasNegativePattern(t *testing.T) {
	if !HasNegativePattern("https://example.com/login") {
		t.Errorf("expected true for /login")
	}
	if !HasNegativePattern("https://example.com/blog/tag/golang") {
		t.Errorf("expected true for /tag/")
	}
	// Verify false positive avoidance on valid technical terms
	if HasNegativePattern("https://example.com/api/cartesian-product") {
		t.Errorf("should not flag /api/cartesian-product as negative")
	}
}

func TestComputePriority(t *testing.T) {
	// High value doc link at depth 0
	pHigh := ComputePriority("https://docs.docker.com/engine/api/", "Docker Engine API Reference", 0)
	if pHigh < 0.75 {
		t.Errorf("expected high priority for Docker API, got %f", pHigh)
	}

	// Junk login link at depth 2
	pJunk := ComputePriority("https://docs.docker.com/login", "Sign in to Docker", 2)
	if pJunk > 0.20 {
		t.Errorf("expected low priority for login link, got %f", pJunk)
	}

	// NaN check
	if math.IsNaN(pHigh) || math.IsInf(pHigh, 0) {
		t.Fatalf("priority produced NaN or Inf: %f", pHigh)
	}
}
