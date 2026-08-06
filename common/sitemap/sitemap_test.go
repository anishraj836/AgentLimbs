package sitemap

import (
	"testing"
)

func TestParseSitemapXML(t *testing.T) {
	sitemapXML := []byte(`<?xml version="1.0" encoding="UTF-8"?>
		<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
			<url>
				<loc>https://example.com/page1</loc>
				<lastmod>2026-01-01</lastmod>
			</url>
			<url>
				<loc>https://example.com/page2</loc>
				<lastmod>2026-01-02</lastmod>
			</url>
		</urlset>
	`)

	urls, err := ParseSitemapXML(sitemapXML)
	if err != nil {
		t.Fatalf("ParseSitemapXML failed: %v", err)
	}

	if len(urls) != 2 {
		t.Fatalf("expected 2 URLs from sitemap, got %d", len(urls))
	}

	if urls[0] != "https://example.com/page1" || urls[1] != "https://example.com/page2" {
		t.Errorf("unexpected sitemap URLs: %v", urls)
	}
}
