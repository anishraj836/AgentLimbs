package crawler

import (
	"context"
	"strings"
	"sync"
	"testing"
)

func TestNormalizeCanonicalURL(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expected      string
		allowLoopback bool
		expectErr     bool
	}{
		{
			name:     "Strip fragment and lowercase scheme/host",
			input:    "HTTPS://Docs.Docker.Com/engine/install#section-1",
			expected: "https://docs.docker.com/engine/install",
		},
		{
			name:     "Strip default ports",
			input:    "https://example.com:443/page",
			expected: "https://example.com/page",
		},
		{
			name:     "Strip default http port",
			input:    "http://example.com:80/page",
			expected: "http://example.com/page",
		},
		{
			name:     "Keep non-default whitelisted port",
			input:    "http://example.com:8080/api",
			expected: "http://example.com:8080/api",
		},
		{
			name:      "Reject disallowed port (SSRF defense)",
			input:     "http://example.com:6379/keys",
			expectErr: true,
		},
		{
			name:     "Strip tracking parameters and sort remaining queries",
			input:    "https://example.com/item?z=2&utm_source=twitter&a=1&utm_medium=social&fbclid=xyz",
			expected: "https://example.com/item?a=1&z=2",
		},
		{
			name:     "Trim trailing slash on non-root paths",
			input:    "https://example.com/docs/",
			expected: "https://example.com/docs",
		},
		{
			name:     "Preserve root slash",
			input:    "https://example.com/",
			expected: "https://example.com/",
		},
		{
			name:      "Drop media resource URLs",
			input:     "https://example.com/images/hero.png",
			expectErr: true,
		},
		{
			name:      "Crawler trap: excessive path segments (>10)",
			input:     "https://example.com/1/2/3/4/5/6/7/8/9/10/11/end",
			expectErr: true,
		},
		{
			name:      "Crawler trap: repeated path segments (>=3)",
			input:     "https://example.com/a/b/a/b/a",
			expectErr: true,
		},
		{
			name:      "Crawler trap: URL exceeding 2048 characters",
			input:     "https://example.com/" + strings.Repeat("toolong/", 300),
			expectErr: true,
		},
		{
			name:          "Zero-DNS SSRF: reject private IP literal",
			input:         "http://192.168.1.1/admin",
			allowLoopback: false,
			expectErr:     true,
		},
		{
			name:          "Zero-DNS SSRF: allow loopback when configured for testing",
			input:         "http://127.0.0.1:8080/test",
			allowLoopback: true,
			expected:      "http://127.0.0.1:8080/test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeCanonicalURL(tt.input, tt.allowLoopback)
			if tt.expectErr {
				if err == nil {
					t.Fatalf("expected error for input %q, got output %q", tt.input, got)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error for input %q: %v", tt.input, err)
				}
				if got != tt.expected {
					t.Errorf("NormalizeCanonicalURL(%q) = %q, want %q", tt.input, got, tt.expected)
				}
			}
		})
	}
}

func TestFrontier_ConcurrentDeduplication(t *testing.T) {
	frontier, err := NewFrontier(FrontierConfig{
		SeedURL:         "https://example.com/start",
		MaxDepth:        3,
		MaxPages:        100,
		AllowSubdomains: false,
		AllowLoopback:   true,
	})
	if err != nil {
		t.Fatalf("NewFrontier failed: %v", err)
	}
	defer frontier.Close()

	var wg sync.WaitGroup
	goroutines := 50
	targetURL := "https://example.com/shared-page?utm_source=test#anchor"

	var enqueuedCount int32
	var mu sync.Mutex

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			enqueued, err := frontier.Enqueue(targetURL, 1)
			if err == nil && enqueued {
				mu.Lock()
				enqueuedCount++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	if enqueuedCount != 1 {
		t.Errorf("expected exactly 1 successful enqueue across %d concurrent goroutines, got %d", goroutines, enqueuedCount)
	}

	// Queue should have seed URL + the 1 enqueued URL = 2 items
	if frontier.Len() != 2 {
		t.Errorf("expected queue length 2, got %d", frontier.Len())
	}
}

func TestFrontier_MaxDepthBoundary(t *testing.T) {
	frontier, err := NewFrontier(FrontierConfig{
		MaxDepth: 2,
		MaxPages: 50,
	})
	if err != nil {
		t.Fatalf("NewFrontier failed: %v", err)
	}
	defer frontier.Close()

	// Enqueue depth 1 -> allowed
	ok, err := frontier.Enqueue("https://example.com/depth-1", 1)
	if err != nil || !ok {
		t.Fatalf("expected depth 1 to enqueue: ok=%v, err=%v", ok, err)
	}

	// Enqueue depth 2 -> allowed
	ok, err = frontier.Enqueue("https://example.com/depth-2", 2)
	if err != nil || !ok {
		t.Fatalf("expected depth 2 to enqueue: ok=%v, err=%v", ok, err)
	}

	// Enqueue depth 3 -> skipped due to maxDepth 2
	ok, err = frontier.Enqueue("https://example.com/depth-3", 3)
	if err != nil || ok {
		t.Fatalf("expected depth 3 to be skipped: ok=%v, err=%v", ok, err)
	}

	if frontier.Len() != 2 {
		t.Errorf("expected 2 items enqueued, got %d", frontier.Len())
	}
}

func TestFrontier_PatternsAndSubdomains(t *testing.T) {
	frontier, err := NewFrontier(FrontierConfig{
		SeedURL:         "https://docs.docker.com",
		MaxDepth:        3,
		MaxPages:        50,
		AllowSubdomains: true,
		IncludePatterns: []string{"/engine/*", "/get-started/*"},
		ExcludePatterns: []string{"*/archive/*"},
	})
	if err != nil {
		t.Fatalf("NewFrontier failed: %v", err)
	}
	defer frontier.Close()

	tests := []struct {
		url      string
		expected bool
	}{
		{"https://docs.docker.com/engine/install", true},
		{"https://sub.docs.docker.com/get-started/overview", true},
		{"https://docs.docker.com/engine/archive/v1", false}, // Excluded by archive pattern
		{"https://docs.docker.com/other/path", false},        // Not in include patterns
		{"https://unrelated.com/engine/install", false},      // Foreign domain
	}

	for _, tt := range tests {
		ok, _ := frontier.Enqueue(tt.url, 1)
		if ok != tt.expected {
			t.Errorf("Enqueue(%q) = %v; want %v", tt.url, ok, tt.expected)
		}
	}
}

func TestFrontier_DequeueCancellation(t *testing.T) {
	frontier, err := NewFrontier(FrontierConfig{
		MaxDepth: 1,
		MaxPages: 10,
	})
	if err != nil {
		t.Fatalf("NewFrontier failed: %v", err)
	}
	defer frontier.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, ok := frontier.Dequeue(ctx)
	if ok {
		t.Errorf("expected Dequeue to return false on cancelled context")
	}
}
