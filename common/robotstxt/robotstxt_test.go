package robotstxt

import (
	"testing"
)

func TestRobotsTxtCompliance(t *testing.T) {
	robotsTxt := `
		User-agent: *
		Disallow: /admin
		Disallow: /private/

		User-agent: AntigravityBot
		Disallow: /internal
	`

	rd := ParseRobotsTxt(robotsTxt)

	if !rd.IsAllowed("AntigravityBot", "/public/index.html") {
		t.Errorf("expected /public to be allowed, got false")
	}

	if rd.IsAllowed("AntigravityBot", "/admin/dashboard") {
		t.Errorf("expected /admin to be disallowed for *, got true")
	}

	if rd.IsAllowed("AntigravityBot", "/internal/secret") {
		t.Errorf("expected /internal to be disallowed for AntigravityBot, got true")
	}
}

func TestEndToEndRobotsDomainCaching(t *testing.T) {
	domain := "testdomain.org"
	robotsContent := `
		User-agent: *
		Disallow: /restricted/
		Disallow: /admin
	`

	// 1. Fetch & Cache domain rules
	GlobalDomainCache.FetchAndCache(domain, robotsContent)

	// 2. Assert allowed public URL
	if !IsAllowed("AntigravityBot", "https://testdomain.org/docs/getting-started") {
		t.Fatalf("expected /docs to be allowed")
	}

	// 3. Assert disallowed restricted path URL
	if IsAllowed("AntigravityBot", "https://testdomain.org/restricted/secret-data") {
		t.Fatalf("expected /restricted/secret-data to be disallowed by robots.txt")
	}

	// 4. Assert disallowed admin path URL
	if IsAllowed("AntigravityBot", "https://testdomain.org/admin/dashboard") {
		t.Fatalf("expected /admin/dashboard to be disallowed by robots.txt")
	}
}
