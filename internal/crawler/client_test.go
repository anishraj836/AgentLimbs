package crawler

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/crawler-monorepo/common/middleware"
	"github.com/crawler-monorepo/common/tracing"
)

func TestOutboundContextHeaderPropagation(t *testing.T) {
	var gotReqID, gotTraceparent string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReqID = r.Header.Get("X-Request-ID")
		gotTraceparent = r.Header.Get("traceparent")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	defer ts.Close()

	client := NewTestClient(true)
	span, ctx := tracing.StartSpan(context.Background(), "test_fetch")
	ctx = context.WithValue(ctx, middleware.RequestIDKey, "req-test-uuid-999")

	_, err := client.Fetch(ctx, ts.URL)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if gotReqID != "req-test-uuid-999" {
		t.Errorf("expected X-Request-ID 'req-test-uuid-999', got '%s'", gotReqID)
	}

	if !strings.HasPrefix(gotTraceparent, "00-"+span.TraceID) {
		t.Errorf("expected traceparent starting with '00-%s', got '%s'", span.TraceID, gotTraceparent)
	}
}

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		ip       string
		expected bool
	}{
		{"127.0.0.1", true},
		{"10.0.0.1", true},
		{"172.16.0.5", true},
		{"192.168.1.1", true},
		{"169.254.169.254", true},
		{"0.0.0.0", true},
		{"0.1.2.3", true},
		{"100.64.0.1", true},
		{"100.127.255.255", true},
		{"100.63.255.255", false},
		{"100.128.0.1", false},
		{"255.255.255.255", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"::1", true},
		{"fc00::1", true},
		{"::127.0.0.1", true},
		{"::192.168.1.1", true},
		{"64:ff9b::192.168.1.1", true},
		{"64:ff9b::10.0.0.1", true},
		{"64:ff9b::8.8.8.8", false},
		{"64:ff9b:1::192.168.1.1", true},
	}

	for _, tt := range tests {
		ip := net.ParseIP(tt.ip)
		if ip == nil {
			t.Fatalf("Failed to parse test IP: %s", tt.ip)
		}
		got := IsPrivateIP(ip)
		if got != tt.expected {
			t.Errorf("IsPrivateIP(%s) = %v; want %v", tt.ip, got, tt.expected)
		}
	}
}

func TestCheckRedirect_SSRFGuard(t *testing.T) {
	client := NewTestClient(false) // do NOT allow loopback

	req, _ := http.NewRequest("GET", "https://example.com/redirect", nil)
	via := []*http.Request{req}

	// 1. Literal private IP redirect
	redirectReqPrivate, _ := http.NewRequest("GET", "http://127.0.0.1/admin", nil)
	err := client.client.CheckRedirect(redirectReqPrivate, via)
	if err == nil || !strings.Contains(err.Error(), "blocked redirect to private IP") {
		t.Errorf("expected blocked redirect for 127.0.0.1, got: %v", err)
	}

	// 2. Localhost hostname redirect
	redirectReqLocalhost, _ := http.NewRequest("GET", "http://localhost/admin", nil)
	err = client.client.CheckRedirect(redirectReqLocalhost, via)
	if err == nil || !strings.Contains(err.Error(), "blocked redirect to private IP") {
		t.Errorf("expected blocked redirect for localhost, got: %v", err)
	}
}

func TestFetchWithStepping_NonRetryableError(t *testing.T) {
	attempts := 0
	mockTransport := &mockRoundTripper{
		fn: func(req *http.Request) (*http.Response, error) {
			attempts++
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(strings.NewReader("Not Found")),
				Request:    req,
			}, nil
		},
	}

	client := NewTestClientWithTransport(mockTransport, true)
	GlobalDomainCache.FetchAndCache("example.com", "User-agent: *\nDisallow:")

	res, err := client.FetchWithStepping(context.Background(), "https://example.com/not-found")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Response.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", res.Response.StatusCode)
	}
	if attempts != 1 {
		t.Errorf("expected exactly 1 attempt for 404 non-retryable response, got %d", attempts)
	}
}

func TestHeaderRotation(t *testing.T) {
	p1 := GetRotatedHeaderProfile()
	p2 := GetRotatedHeaderProfile()

	if p1.UserAgent == "" || p2.UserAgent == "" {
		t.Errorf("Expected non-empty User-Agent strings")
	}

	req, _ := http.NewRequest("GET", "https://example.com", nil)
	ApplyAntiBotHeaders(req, p1)

	if req.Header.Get("User-Agent") != p1.UserAgent {
		t.Errorf("User-Agent header not set correctly")
	}
	if req.Header.Get("Sec-Ch-Ua") != p1.SecChUa {
		t.Errorf("Sec-Ch-Ua header not set correctly")
	}
}

func TestIsSPAPlaceholder(t *testing.T) {
	if !IsSPAPlaceholder(`<div id="root"></div>`) {
		t.Errorf("Expected SPA placeholder for root div")
	}
	if !IsSPAPlaceholder(`<div id="app"></div>`) {
		t.Errorf("Expected SPA placeholder for app div")
	}
	if IsSPAPlaceholder(`<html><body><h1>Hello World</h1><p>Full content</p></body></html>`) {
		t.Errorf("Expected full content HTML not to be SPA placeholder")
	}
}

type mockRoundTripper struct {
	fn func(req *http.Request) (*http.Response, error)
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.fn(req)
}

func TestFetchWithStepping(t *testing.T) {
	attempts := 0
	mockTransport := &mockRoundTripper{
		fn: func(req *http.Request) (*http.Response, error) {
			attempts++
			if attempts == 1 {
				// First attempt returns 403 Forbidden
				return &http.Response{
					StatusCode: http.StatusForbidden,
					Body:       io.NopCloser(strings.NewReader("Forbidden")),
					Request:    req,
				}, nil
			}
			// Second attempt (stepped) returns 200 OK
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("OK Content")),
				Request:    req,
			}, nil
		},
	}

	client := NewTestClientWithTransport(mockTransport, true)

	// Pre-seed robots cache to allow domain
	GlobalDomainCache.FetchAndCache("example.com", "User-agent: *\nDisallow:")

	res, err := client.FetchWithStepping(context.Background(), "https://example.com/test")
	if err != nil {
		t.Fatalf("FetchWithStepping failed: %v", err)
	}

	if res.Response.StatusCode != http.StatusOK {
		t.Errorf("Expected status code 200 after stepping, got %d", res.Response.StatusCode)
	}

	if attempts < 2 {
		t.Errorf("Expected at least 2 attempts due to stepping, got %d", attempts)
	}
}

func TestRobotsTxtParser(t *testing.T) {
	robotsTxt := `
User-agent: AntigravityBot
Disallow: /private/
Disallow: /admin

User-agent: *
Disallow: /blocked/
`
	rd := ParseRobotsTxt(robotsTxt)

	if rd.IsAllowed("AntigravityBot", "https://example.com/public/page") == false {
		t.Errorf("Expected /public/page to be allowed for AntigravityBot")
	}
	if rd.IsAllowed("AntigravityBot", "https://example.com/private/secret") == true {
		t.Errorf("Expected /private/secret to be disallowed for AntigravityBot")
	}
	if rd.IsAllowed("OtherBot", "https://example.com/blocked/page") == true {
		t.Errorf("Expected /blocked/page to be disallowed for OtherBot")
	}
}

func TestRobotsTxtParser_MultipleUserAgentsAndSpecificity(t *testing.T) {
	robotsTxt := `
# Block with multiple user-agents sharing rules
User-agent: Googlebot
User-agent: AntigravityBot
Disallow: /restricted/

# Wildcard block
User-agent: *
Disallow: /restricted/
Disallow: /general-block/
`
	rd := ParseRobotsTxt(robotsTxt)

	// AntigravityBot matches specific block: /restricted/ is disallowed, /general-block/ is allowed
	if rd.IsAllowed("AntigravityBot/1.0", "https://example.com/restricted/doc") != false {
		t.Errorf("Expected /restricted/doc to be disallowed for AntigravityBot")
	}
	if rd.IsAllowed("AntigravityBot/1.0", "https://example.com/general-block/doc") != true {
		t.Errorf("Expected /general-block/doc to be allowed for AntigravityBot due to higher specificity block")
	}

	// UnknownBot falls back to wildcard block where /general-block/ is disallowed
	if rd.IsAllowed("UnknownBot/1.0", "https://example.com/general-block/doc") != false {
		t.Errorf("Expected /general-block/doc to be disallowed for UnknownBot")
	}
}

func TestDomainCacheManager_CapacityAndEviction(t *testing.T) {
	cm := &DomainCacheManager{
		cache:      make(map[string]*RobotsData),
		expiry:     make(map[string]time.Time),
		maxEntries: 5,
	}

	for i := 1; i <= 10; i++ {
		domain := fmt.Sprintf("domain%d.com", i)
		cm.FetchAndCache(domain, "User-agent: *\nDisallow: /admin")
	}

	cm.mu.RLock()
	cacheLen := len(cm.cache)
	cm.mu.RUnlock()

	if cacheLen > 5 {
		t.Errorf("Expected cache size bounded at 5, got %d", cacheLen)
	}
}

func TestFetchWithStepping_HeaderPropagation(t *testing.T) {
	var receivedReqID, receivedTraceparent string
	mockTransport := &mockRoundTripper{
		fn: func(req *http.Request) (*http.Response, error) {
			receivedReqID = req.Header.Get("X-Request-ID")
			receivedTraceparent = req.Header.Get("traceparent")
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("OK")),
				Request:    req,
			}, nil
		},
	}

	client := NewTestClientWithTransport(mockTransport, true)
	GlobalDomainCache.FetchAndCache("example.com", "User-agent: *\nDisallow:")

	span, ctx := tracing.StartSpan(context.Background(), "test_stepping")
	ctx = context.WithValue(ctx, middleware.RequestIDKey, "req-step-12345")

	_, err := client.FetchWithStepping(ctx, "https://example.com/test-stepping")
	if err != nil {
		t.Fatalf("FetchWithStepping failed: %v", err)
	}

	if receivedReqID != "req-step-12345" {
		t.Errorf("Expected X-Request-ID 'req-step-12345', got %q", receivedReqID)
	}
	if !strings.HasPrefix(receivedTraceparent, "00-"+span.TraceID) {
		t.Errorf("Expected traceparent starting with '00-%s', got %q", span.TraceID, receivedTraceparent)
	}
}

func TestIsMediaResourceURL(t *testing.T) {
	blockedURLs := []string{
		"https://example.com/image.png",
		"https://example.com/photo.JPEG",
		"https://example.com/icon.svg?v=1",
		"https://example.com/banner.webp",
		"https://example.com/font.woff2",
		"https://example.com/video.mp4",
		"https://example.com/audio.mp3",
		"https://example.com/image.png;matrix_param=123",
		"https://example.com/video.mp4/",
		"https://example.com/audio.mp3;jsessionid=456/",
	}

	for _, u := range blockedURLs {
		if !IsMediaResourceURL(u) {
			t.Errorf("Expected %s to be recognized as media resource URL", u)
		}
	}

	allowedURLs := []string{
		"https://example.com/style.css",
		"https://example.com/app.js",
		"https://example.com/module.mjs",
		"https://example.com/data.json",
		"https://example.com/index.html",
		"https://example.com/about",
		"https://example.com/about/",
		"https://example.com/page.html;jsessionid=789",
	}

	for _, u := range allowedURLs {
		if IsMediaResourceURL(u) {
			t.Errorf("Expected %s NOT to be recognized as media resource URL", u)
		}
	}
}

func TestFetchBlockedMediaResource(t *testing.T) {
	client := NewTestClient(true)
	GlobalDomainCache.FetchAndCache("example.com", "User-agent: *\nDisallow:")

	_, err := client.Fetch(context.Background(), "https://example.com/image.png")
	if err == nil {
		t.Fatalf("Expected error when fetching media URL, got nil")
	}
	if !strings.Contains(err.Error(), "blocked media resource") {
		t.Errorf("Expected blocked media error message, got: %v", err)
	}
}

func TestStrictHostMatchCookieScoping(t *testing.T) {
	receivedHeaders := make(map[string]http.Header)
	mockTransport := &mockRoundTripper{
		fn: func(req *http.Request) (*http.Response, error) {
			receivedHeaders[req.URL.String()] = req.Header.Clone()
			if req.URL.String() == "https://domaina.com/redirect-cross" {
				return &http.Response{
					StatusCode: http.StatusFound,
					Header: http.Header{
						"Location": []string{"https://domainb.com/target"},
					},
					Request: req,
				}, nil
			}
			if req.URL.String() == "https://domaina.com/redirect-same" {
				return &http.Response{
					StatusCode: http.StatusFound,
					Header: http.Header{
						"Location": []string{"https://domaina.com/target"},
					},
					Request: req,
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("OK")),
				Request:    req,
			}, nil
		},
	}

	client := NewTestClientWithTransport(mockTransport, true)

	// Case 1: Cross-host redirect (domaina.com -> domainb.com)
	reqCross, _ := http.NewRequest("GET", "https://domaina.com/redirect-cross", nil)
	reqCross.Header.Set("Cookie", "session_id=secret123")
	reqCross.Header.Set("Authorization", "Bearer secret_token")
	_, err := client.client.Do(reqCross)
	if err != nil {
		t.Fatalf("Do failed for cross-host redirect: %v", err)
	}

	targetHeaderCross := receivedHeaders["https://domainb.com/target"]
	if targetHeaderCross.Get("Cookie") != "" {
		t.Errorf("Expected Cookie header to be stripped on cross-host redirect, got: %s", targetHeaderCross.Get("Cookie"))
	}
	if targetHeaderCross.Get("Authorization") != "" {
		t.Errorf("Expected Authorization header to be stripped on cross-host redirect, got: %s", targetHeaderCross.Get("Authorization"))
	}

	// Case 2: Same-host redirect (domaina.com -> domaina.com)
	reqSame, _ := http.NewRequest("GET", "https://domaina.com/redirect-same", nil)
	reqSame.Header.Set("Cookie", "session_id=secret123")
	_, err = client.client.Do(reqSame)
	if err != nil {
		t.Fatalf("Do failed for same-host redirect: %v", err)
	}

	targetHeaderSame := receivedHeaders["https://domaina.com/target"]
	if targetHeaderSame.Get("Cookie") != "session_id=secret123" {
		t.Errorf("Expected Cookie header to be retained on same-host redirect, got: %s", targetHeaderSame.Get("Cookie"))
	}
}

func TestFetchSmart_SPARendering(t *testing.T) {
	mockTransport := &mockRoundTripper{
		fn: func(req *http.Request) (*http.Response, error) {
			// Returns SPA placeholder div
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`<div id="root"></div>`)),
				Request:    req,
			}, nil
		},
	}

	client := NewTestClientWithTransport(mockTransport, true)
	GlobalDomainCache.FetchAndCache("example.com", "User-agent: *\nDisallow:")

	// 1. Automatic SPA rendering trigger when SPA placeholder detected
	res, err := client.FetchSmart(context.Background(), "https://example.com/spa-page", false)
	if err != nil {
		t.Fatalf("FetchSmart failed: %v", err)
	}
	defer res.Response.Body.Close()

	bodyBytes, _ := io.ReadAll(res.Response.Body)
	bodyStr := string(bodyBytes)

	if !strings.Contains(bodyStr, "Rendered Content for https://example.com/spa-page") {
		t.Errorf("Expected rendered SPA content, got: %s", bodyStr)
	}

	// 2. Forced JS rendering
	resForced, err := client.FetchSmart(context.Background(), "https://example.com/forced-js", true)
	if err != nil {
		t.Fatalf("FetchSmart with forceJS failed: %v", err)
	}
	defer resForced.Response.Body.Close()

	forcedBytes, _ := io.ReadAll(resForced.Response.Body)
	forcedStr := string(forcedBytes)

	if !strings.Contains(forcedStr, "Rendered Content for https://example.com/forced-js") {
		t.Errorf("Expected forced rendered SPA content, got: %s", forcedStr)
	}
}
