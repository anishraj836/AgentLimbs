package crawler

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
)

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
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"::1", true},
		{"fc00::1", true},
	}

	for _, tt := range tests {
		ip := net.ParseIP(tt.ip)
		got := IsPrivateIP(ip)
		if got != tt.expected {
			t.Errorf("IsPrivateIP(%s) = %v; want %v", tt.ip, got, tt.expected)
		}
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
