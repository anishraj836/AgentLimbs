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
