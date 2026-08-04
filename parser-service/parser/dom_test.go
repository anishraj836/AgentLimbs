package parser

import (
	"testing"
)

func TestExtractLinks(t *testing.T) {
	html := []byte(`
		<!DOCTYPE html>
		<html>
		<body>
			<a href="/about">About Us</a>
			<a href="/about">About Us Again</a>
			<a href="https://example.com/contact">Contact</a>
			<a href="../privacy">Privacy Policy</a>
			<a href="https://EXAMPLE.COM/about/#top">Canonical duplicate</a>
			<a href="mailto:hello@example.com">Email</a>
			<a href="javascript:void(0)">Script</a>
			<a href="ftp://files.example.com/file.txt">FTP</a>
			<a href="">Self Link</a>
			<a href="#section1">Self Fragment</a>
		</body>
		</html>
	`)

	links, err := ExtractLinks("https://example.com/docs/", html)
	if err != nil {
		t.Fatalf("ExtractLinks failed: %v", err)
	}

	expected := map[string]bool{
		"https://example.com/about":   true,
		"https://example.com/contact": true,
		"https://example.com/privacy": true,
	}

	if len(links) != len(expected) {
		t.Errorf("expected %d links, got %d", len(expected), len(links))
	}

	for _, l := range links {
		if !expected[l] {
			t.Errorf("unexpected link extracted: %s", l)
		}
	}
}
