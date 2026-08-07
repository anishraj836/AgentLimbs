package robotstxt

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
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

func TestRobotsTxtCacheStampede(t *testing.T) {
	var fetchCount int32
	domain := "stampede-domain.com"
	robotsContent := `
		User-agent: *
		Disallow: /secret
	`

	fetchFunc := func(dom string) (string, error) {
		atomic.AddInt32(&fetchCount, 1)
		// Introduce brief sleep to ensure concurrent goroutines hit singleflight simultaneously
		time.Sleep(50 * time.Millisecond)
		return robotsContent, nil
	}

	const numGoroutines = 20
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			_, err := EnsureRobotsCached(domain, fetchFunc)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}

	wg.Wait()

	count := atomic.LoadInt32(&fetchCount)
	if count != 1 {
		t.Fatalf("expected fetchFunc to be called EXACTLY ONCE (counter == 1), got %d", count)
	}

	// Verify that domain is cached properly
	if IsAllowed("AntigravityBot", "https://stampede-domain.com/secret") {
		t.Errorf("expected /secret to be disallowed after caching")
	}
}
