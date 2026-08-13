package crawler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sync"
	"testing"
)

func TestMultiTierProxyManager_GetProxy(t *testing.T) {
	dcProxies := []string{"http://dc1.proxy.com:8080", "http://dc2.proxy.com:8080"}
	resProxies := []string{"http://res1.proxy.com:8080", "http://res2.proxy.com:8080"}

	pm, err := NewMultiTierProxyManager(dcProxies, resProxies)
	if err != nil {
		t.Fatalf("Failed to create MultiTierProxyManager: %v", err)
	}

	targetURL := "https://example.com/api"

	// Tier 1: Direct Egress (failedAttempts = 0)
	p1, err := pm.GetProxy(targetURL, 0)
	if err != nil {
		t.Fatalf("Unexpected error for Tier 1: %v", err)
	}
	if p1 != nil {
		t.Errorf("Expected nil proxy for Tier 1 direct egress, got: %v", p1)
	}

	// Tier 2: Datacenter Proxy (failedAttempts = 1)
	p2_1, err := pm.GetProxy(targetURL, 1)
	if err != nil || p2_1 == nil {
		t.Fatalf("Expected DC proxy for failedAttempts=1, got err: %v, proxy: %v", err, p2_1)
	}
	if p2_1.String() != "http://dc1.proxy.com:8080" {
		t.Errorf("Expected dc1.proxy.com, got: %s", p2_1.String())
	}

	p2_2, err := pm.GetProxy(targetURL, 1)
	if err != nil || p2_2 == nil {
		t.Fatalf("Expected DC proxy 2 for failedAttempts=1, got err: %v, proxy: %v", err, p2_2)
	}
	if p2_2.String() != "http://dc2.proxy.com:8080" {
		t.Errorf("Expected dc2.proxy.com, got: %s", p2_2.String())
	}

	// Tier 3: Residential Proxy (failedAttempts >= 2)
	p3_1, err := pm.GetProxy(targetURL, 2)
	if err != nil || p3_1 == nil {
		t.Fatalf("Expected Res proxy for failedAttempts=2, got err: %v, proxy: %v", err, p3_1)
	}
	if p3_1.String() != "http://res1.proxy.com:8080" {
		t.Errorf("Expected res1.proxy.com, got: %s", p3_1.String())
	}

	p3_2, err := pm.GetProxy(targetURL, 3)
	if err != nil || p3_2 == nil {
		t.Fatalf("Expected Res proxy 2 for failedAttempts=3, got err: %v, proxy: %v", err, p3_2)
	}
	if p3_2.String() != "http://res2.proxy.com:8080" {
		t.Errorf("Expected res2.proxy.com, got: %s", p3_2.String())
	}
}

func TestMultiTierProxyManager_Fallback(t *testing.T) {
	// Only DC proxies available
	pmDC, _ := NewMultiTierProxyManager([]string{"http://dc1.proxy.com:8080"}, nil)
	// Failed attempts = 2 (Tier 3), should fallback to DC
	pRes, err := pmDC.GetProxy("https://example.com", 2)
	if err != nil || pRes == nil {
		t.Fatalf("Expected fallback to DC proxy, got err: %v, proxy: %v", err, pRes)
	}
	if pRes.String() != "http://dc1.proxy.com:8080" {
		t.Errorf("Expected fallback to dc1, got %s", pRes.String())
	}

	// Only Res proxies available
	pmRes, _ := NewMultiTierProxyManager(nil, []string{"http://res1.proxy.com:8080"})
	// Failed attempts = 1 (Tier 2), should fallback to Res
	pDC, err := pmRes.GetProxy("https://example.com", 1)
	if err != nil || pDC == nil {
		t.Fatalf("Expected fallback to Res proxy, got err: %v, proxy: %v", err, pDC)
	}
	if pDC.String() != "http://res1.proxy.com:8080" {
		t.Errorf("Expected fallback to res1, got %s", pDC.String())
	}
}

func TestProxyTransport_2MBPayloadCap(t *testing.T) {
	// Create 3MB response payload
	threeMB := bytes.Repeat([]byte("A"), 3*1024*1024)

	mockTransport := &mockRoundTripper{
		fn: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader(threeMB)),
				Request:    req,
			}, nil
		},
	}

	pm, _ := NewMultiTierProxyManager([]string{"http://127.0.0.1:8080"}, nil)
	pm.AllowLoopback = true
	wrappedTr := pm.WrapTransport(mockTransport)

	req, _ := http.NewRequestWithContext(context.Background(), "GET", "https://example.com/large", nil)
	resp, err := wrappedTr.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip failed: %v", err)
	}
	defer resp.Body.Close()

	readData, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read capped body: %v", err)
	}

	expectedCap := 2 * 1024 * 1024
	if len(readData) != expectedCap {
		t.Errorf("Expected payload capped at %d bytes (2MB), got %d bytes", expectedCap, len(readData))
	}
}

func TestMultiTierProxyManager_Concurrency(t *testing.T) {
	pm, _ := NewMultiTierProxyManager(
		[]string{"http://dc1.com:8080", "http://dc2.com:8080"},
		[]string{"http://res1.com:8080", "http://res2.com:8080"},
	)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(attempt int) {
			defer wg.Done()
			_, _ = pm.GetProxy("https://example.com", attempt%3)
		}(i)
	}
	wg.Wait()
}
