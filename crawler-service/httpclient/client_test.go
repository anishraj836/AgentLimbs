package httpclient

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/crawler-monorepo/common/robotstxt"
)

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
		if !isPrivateIP(ip) {
			t.Errorf("expected isPrivateIP(%s) to be true, got false", ipStr)
		}
	}

	publicIPs := []string{
		"8.8.8.8",
		"1.1.1.1",
		"93.184.216.34",
	}

	for _, ipStr := range publicIPs {
		ip := net.ParseIP(ipStr)
		if isPrivateIP(ip) {
			t.Errorf("expected isPrivateIP(%s) to be false, got true", ipStr)
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
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	defer ts.Close()

	client := NewClient()
	client.AllowLoopbackForTesting = true // Enable loopback for local httptest.Server
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
