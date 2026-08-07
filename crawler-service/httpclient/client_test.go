package httpclient

import (
	"net"
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
