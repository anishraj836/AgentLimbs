package crawler

import (
	"sync"
	"testing"
)

func TestPriorityFrontier_MaxHeapOrder(t *testing.T) {
	cfg := PriorityFrontierConfig{
		SeedURL:       "https://go.dev/doc/",
		MaxDepth:      3,
		QueueCapacity: 100,
		MinPriority:   0.05,
		AllowLoopback: true,
	}

	pf, err := NewPriorityFrontier(cfg)
	if err != nil {
		t.Fatalf("NewPriorityFrontier failed: %v", err)
	}

	// Enqueue URLs with varying priorities
	_, _ = pf.Enqueue("https://go.dev/login", "login", 2)                                 // Low priority
	_, _ = pf.Enqueue("https://go.dev/doc/tutorial/basics.html", "Getting Started", 1)    // High priority
	_, _ = pf.Enqueue("https://go.dev/doc/architecture.html", "Architecture Guide", 1)   // Very high priority

	item1, ok := pf.TryDequeue()
	if !ok || item1 == nil {
		t.Fatalf("expected item1, got nil")
	}

	item2, ok := pf.TryDequeue()
	if !ok || item2 == nil {
		t.Fatalf("expected item2, got nil")
	}

	item3, ok := pf.TryDequeue()
	if !ok || item3 == nil {
		t.Fatalf("expected item3, got nil")
	}

	// Verify monotonic priority descent
	if item1.Priority < item2.Priority || item2.Priority < item3.Priority {
		t.Fatalf("priority order violation: item1=%f, item2=%f, item3=%f",
			item1.Priority, item2.Priority, item3.Priority)
	}

	t.Logf("Dequeued in order: 1) %s (P=%.4f), 2) %s (P=%.4f), 3) %s (P=%.4f)",
		item1.URL, item1.Priority, item2.URL, item2.Priority, item3.URL, item3.Priority)
}

func TestPriorityFrontier_ConcurrentDeduplication(t *testing.T) {
	cfg := PriorityFrontierConfig{
		SeedURL:       "https://example.com/",
		MaxDepth:      2,
		QueueCapacity: 500,
		MinPriority:   0.05,
		AllowLoopback: true,
	}

	pf, err := NewPriorityFrontier(cfg)
	if err != nil {
		t.Fatalf("NewPriorityFrontier failed: %v", err)
	}

	var wg sync.WaitGroup
	workers := 50
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = pf.Enqueue("https://example.com/docs/api.html", "API Docs", 1)
		}()
	}
	wg.Wait()

	if pf.Len() != 1 {
		t.Fatalf("expected exactly 1 item in priority queue, got %d", pf.Len())
	}
}
