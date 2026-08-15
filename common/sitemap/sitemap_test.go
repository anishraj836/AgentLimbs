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

func TestParseSitemapIndexXML(t *testing.T) {
	sitemapIndexXML := []byte(`<?xml version="1.0" encoding="UTF-8"?>
		<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
			<sitemap>
				<loc>https://example.com/sitemap1.xml</loc>
			</sitemap>
			<sitemap>
				<loc>https://example.com/sitemap2.xml</loc>
			</sitemap>
		</sitemapindex>
	`)

	urls, err := ParseSitemapXML(sitemapIndexXML)
	if err != nil {
		t.Fatalf("ParseSitemapXML for index failed: %v", err)
	}

	if len(urls) != 2 {
		t.Fatalf("expected 2 child sitemap URLs, got %d", len(urls))
	}

	if urls[0] != "https://example.com/sitemap1.xml" || urls[1] != "https://example.com/sitemap2.xml" {
		t.Errorf("unexpected sitemap index URLs: %v", urls)
	}
}

func TestParseSitemapCorruptXML(t *testing.T) {
	corruptXML := []byte(`<?xml version="1.0" encoding="UTF-8"?>
		<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
			<url>
				<loc>https://example.com/page1</loc>
			<!-- unclosed tag
	`)

	_, err := ParseSitemapXML(corruptXML)
	if err == nil {
		t.Fatalf("expected error for corrupt XML, got nil")
	}
}

func TestParseSitemapInvalidRoot(t *testing.T) {
	htmlXML := []byte(`<html><body><h1>Not a sitemap</h1></body></html>`)

	_, err := ParseSitemapXML(htmlXML)
	if err == nil {
		t.Fatalf("expected error for non-sitemap root element, got nil")
	}
}

func TestParseSitemapSchemeValidationAndTrimming(t *testing.T) {
	xmlData := []byte(`<?xml version="1.0" encoding="UTF-8"?>
		<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
			<url>
				<loc>  https://example.com/valid-page  </loc>
			</url>
			<url>
				<loc>http://example.com/valid-http</loc>
			</url>
			<url>
				<loc>ftp://example.com/invalid-scheme</loc>
			</url>
			<url>
				<loc>javascript:alert(1)</loc>
			</url>
			<url>
				<loc>/relative/path/no/host</loc>
			</url>
		</urlset>
	`)

	urls, err := ParseSitemapXML(xmlData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(urls) != 2 {
		t.Fatalf("expected 2 valid http/https URLs, got %d: %v", len(urls), urls)
	}

	if urls[0] != "https://example.com/valid-page" {
		t.Errorf("expected trimmed 'https://example.com/valid-page', got '%s'", urls[0])
	}
	if urls[1] != "http://example.com/valid-http" {
		t.Errorf("expected 'http://example.com/valid-http', got '%s'", urls[1])
	}
}

func TestParseSitemapEmptyInput(t *testing.T) {
	_, err := ParseSitemapXML([]byte{})
	if err == nil {
		t.Errorf("expected error for empty XML bytes, got nil")
	}
}
