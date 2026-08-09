package httpclient

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/crawler-monorepo/common/robotstxt"
	"github.com/crawler-monorepo/internal/crawler"
)

func setAllowLoopback(c *crawler.Client, allow bool) {
	val := reflect.ValueOf(c).Elem().FieldByName("allowLoopbackForTesting")
	reflect.NewAt(val.Type(), unsafe.Pointer(val.UnsafeAddr())).Elem().SetBool(allow)
}

func TestPrivateIPGuard(t *testing.T) {
	privateIPs := []string{
		"127.0.0.1",
		"10.0.0.1",
		"172.16.0.1",
		"192.168.1.1",
		"169.254.169.254", // AWS Metadata
	}

	for _, ipStr := range privateIPs {
		ip := net.ParseIP(ipStr)
		if !crawler.IsPrivateIP(ip) {
			t.Errorf("expected IsPrivateIP(%s) to be true, got false", ipStr)
		}
	}

	publicIPs := []string{
		"8.8.8.8",
		"1.1.1.1",
		"93.184.216.34",
	}

	for _, ipStr := range publicIPs {
		ip := net.ParseIP(ipStr)
		if crawler.IsPrivateIP(ip) {
			t.Errorf("expected IsPrivateIP(%s) to be false, got true", ipStr)
		}
	}
}

func TestRobotsTxtNetworkFetchAndDisallow(t *testing.T) {
	domain := "example.com"
	rawRobots := "User-agent: *\nDisallow: /private/\n"

	// 1. Fetch & Cache robots.txt for domain
	robotstxt.GlobalDomainCache.FetchAndCache(domain, rawRobots)

	// 2. Test public URL -> Should be ALLOWED
	if !robotstxt.IsAllowed("AntigravityBot", "https://example.com/public/data") {
		t.Fatalf("expected public URL to be allowed by robots.txt")
	}

	// 3. Test private URL -> Should be DISALLOWED
	if robotstxt.IsAllowed("AntigravityBot", "https://example.com/private/data") {
		t.Fatalf("expected private URL to be disallowed by robots.txt")
	}
}

func TestEndToEndHTTPTestServerRobotsGating(t *testing.T) {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("skipping local listener test due to environment sandbox: %v", err)
		return
	}

	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("User-agent: *\nDisallow: /blocked/\n"))
		case "/allowed/page":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK Content"))
		case "/blocked/page":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Blocked Content"))
		}
	}))
	ts.Listener = l
	ts.Start()
	defer ts.Close()

	client := NewClient()
	setAllowLoopback(client, true)
	ctx := context.Background()

	// 1. Fetch allowed URL -> Must succeed
	res, err := client.Fetch(ctx, ts.URL+"/allowed/page")
	if err != nil {
		t.Fatalf("expected /allowed/page to succeed, got: %v", err)
	}
	res.Response.Body.Close()

	// 2. Fetch blocked URL -> Must trigger EnsureRobotsCached, parse Disallow: /blocked/, and FAIL
	_, err = client.Fetch(ctx, ts.URL+"/blocked/page")
	if err == nil {
		t.Fatalf("expected /blocked/page to fail due to robots.txt disallow rule, but it succeeded")
	}
	if !strings.Contains(err.Error(), "crawling disallowed by robots.txt rules") {
		t.Fatalf("expected disallowed error message, got: %v", err)
	}
}

func TestAntiBotHeaderInjection(t *testing.T) {
	req, err := http.NewRequest("GET", "https://example.com", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	profile := GetRotatedHeaderProfile()
	ApplyAntiBotHeaders(req, profile)

	headers := []string{
		"User-Agent",
		"Sec-Ch-Ua",
		"Sec-Ch-Ua-Mobile",
		"Sec-Ch-Ua-Platform",
		"Sec-Fetch-Dest",
		"Sec-Fetch-Mode",
		"Sec-Fetch-Site",
		"Accept",
		"Accept-Language",
	}

	for _, h := range headers {
		if req.Header.Get(h) == "" {
			t.Errorf("expected header %s to be set, but was empty", h)
		}
	}

	if !strings.Contains(req.Header.Get("User-Agent"), "Chrome/122.0.0.0") {
		t.Errorf("expected Chrome 122 user agent, got: %s", req.Header.Get("User-Agent"))
	}
}

func TestIsSPAPlaceholderDetection(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected bool
	}{
		{"Empty string", "", true},
		{"Root div empty", "<html><body><div id=\"root\"></div></body></html>", true},
		{"App div empty", "<html><body><div id=\"app\"></div></body></html>", true},
		{"Next div empty", "<html><body><div id=\"__next\"></div></body></html>", true},
		{"Self closing root", "<div id=\"root\"/>", true},
		{"Full content HTML", "<html><body><div><h1>Welcome</h1><p>Full article content here with substantial text.</p></div></body></html>", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsSPAPlaceholder(tt.html)
			if got != tt.expected {
				t.Errorf("IsSPAPlaceholder(%q) = %v, want %v", tt.name, got, tt.expected)
			}
		})
	}
}

func TestSteppingRetryBackoff(t *testing.T) {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("skipping test due to environment sandbox: %v", err)
		return
	}

	attempts := 0
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		// Verify stepped request contains stealth headers
		if r.Header.Get("Sec-Fetch-Dest") == "" {
			t.Errorf("expected stepped request to have Sec-Fetch-Dest header")
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Stepped OK"))
	}))
	ts.Listener = l
	ts.Start()
	defer ts.Close()

	// Pre-seed domain cache for test server URL to bypass robots.txt fetch
	robotstxt.GlobalDomainCache.FetchAndCache("127.0.0.1", "User-agent: *\nAllow: /\n")

	client := NewClient()
	setAllowLoopback(client, true)
	ctx := context.Background()

	start := time.Now()
	res, err := client.FetchWithStepping(ctx, ts.URL+"/allowed")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("expected FetchWithStepping to succeed on step-up retry, got: %v", err)
	}
	res.Response.Body.Close()

	if attempts < 2 {
		t.Errorf("expected at least 2 attempts (initial + stepped retry), got: %d", attempts)
	}
	if elapsed < 50*time.Millisecond {
		t.Errorf("expected jitter backoff delay >= 50ms, got elapsed time: %v", elapsed)
	}
}

