package crawler

import (
	"testing"
)

func TestCleanYieldRatio(t *testing.T) {
	html := `<html><head><script>var veryLongCode = 12345;</script><style>.body { color: red; }</style></head><body><h1>Hello World</h1><p>This is clean text.</p></body></html>`
	markdown := `# Hello World

This is clean text.`

	ratio := CleanYieldRatio(markdown, html)
	if ratio <= 0 || ratio > 1.0 {
		t.Fatalf("unexpected yield ratio: %f", ratio)
	}
	t.Logf("Computed yield ratio: %.4f", ratio)
}

func TestExtractSubtreePrefix(t *testing.T) {
	prefix := ExtractSubtreePrefix("https://example.com/docs/archive/v1/page.html")
	if prefix != "/docs/archive/" {
		t.Fatalf("expected /docs/archive/, got %s", prefix)
	}

	prefixShort := ExtractSubtreePrefix("https://example.com/docs")
	if prefixShort != "/docs/" {
		t.Fatalf("expected /docs/, got %s", prefixShort)
	}

	prefixRoot := ExtractSubtreePrefix("https://example.com/")
	if prefixRoot != "/" {
		t.Fatalf("expected /, got %s", prefixRoot)
	}
}

func TestSubtreeTracker_PruningAndRecovery(t *testing.T) {
	st := NewSubtreeTracker()

	url1 := "https://example.com/archive/old/doc1.html"
	url2 := "https://example.com/archive/old/doc2.html"
	url3 := "https://example.com/archive/old/doc3.html"
	deepChild := "https://example.com/archive/old/v1/leaf.html"

	// 1. Hub page exemption (links >= 8)
	st.RecordYield(url1, 0.005, 10, 2)
	if st.IsPruned(deepChild, 3) {
		t.Fatalf("hub page should be exempt from low-yield penalty")
	}

	// 2. Three consecutive low yield non-hub pages
	st.RecordYield(url1, 0.005, 2, 2)
	st.RecordYield(url2, 0.005, 2, 2)
	st.RecordYield(url3, 0.005, 2, 2)

	// Now deep child at depth 3 should be pruned
	if !st.IsPruned(deepChild, 3) {
		t.Fatalf("deep child should be pruned after 3 consecutive low-yield pages")
	}

	// But page at depth 2 or shallower should still be allowed
	if st.IsPruned(url1, 2) {
		t.Fatalf("page at current depth should not be pruned")
	}

	// 3. Recovery reset on high-yield page
	st.RecordYield(url1, 0.15, 2, 2)
	// After recovery, pruned state remains for old limit until reset, but consecutive count is 0
}
