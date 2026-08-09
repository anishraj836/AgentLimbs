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

	client := NewClient()
	client.client.Transport = mockTransport
	client.allowLoopbackForTesting = true

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
