package crawler

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseSitemapContent_StandardXML(t *testing.T) {
	xmlData := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
   <url>
      <loc>https://example.com/page1</loc>
   </url>
   <url>
      <loc>https://example.com/page2</loc>
   </url>
</urlset>`)

	pages, children, err := ParseSitemapContent(xmlData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(children) != 0 {
		t.Errorf("expected 0 child sitemaps, got %d", len(children))
	}

	if len(pages) != 2 {
		t.Fatalf("expected 2 page URLs, got %d", len(pages))
	}

	if pages[0] != "https://example.com/page1" || pages[1] != "https://example.com/page2" {
		t.Errorf("unexpected page URLs: %v", pages)
	}
}

func TestParseSitemapContent_TransparentGzip(t *testing.T) {
	rawXML := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
   <url>
      <loc>https://example.com/gzipped-page-1</loc>
   </url>
   <url>
      <loc>https://example.com/gzipped-page-2</loc>
   </url>
</urlset>`

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write([]byte(rawXML)); err != nil {
		t.Fatalf("failed to gzip write: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("failed to gzip close: %v", err)
	}

	pages, children, err := ParseSitemapContent(buf.Bytes())
	if err != nil {
		t.Fatalf("unexpected error parsing gzipped sitemap: %v", err)
	}

	if len(children) != 0 {
		t.Errorf("expected 0 child sitemaps, got %d", len(children))
	}

	if len(pages) != 2 {
		t.Fatalf("expected 2 page URLs, got %d", len(pages))
	}

	if pages[0] != "https://example.com/gzipped-page-1" {
		t.Errorf("unexpected page 0: %s", pages[0])
	}
}

func TestFetchAndParseSitemap_RecursiveSitemapIndex(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sitemap_index.xml":
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
   <sitemap>
      <loc>%s/sub_sitemap_1.xml</loc>
   </sitemap>
   <sitemap>
      <loc>%s/sub_sitemap_2.xml.gz</loc>
   </sitemap>
</sitemapindex>`, server.URL, server.URL)

		case "/sub_sitemap_1.xml":
			w.Header().Set("Content-Type", "application/xml")
			w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
   <url><loc>https://example.com/item1</loc></url>
   <url><loc>https://example.com/item2</loc></url>
</urlset>`))

		case "/sub_sitemap_2.xml.gz":
			var gzBuf bytes.Buffer
			gw := gzip.NewWriter(&gzBuf)
			gw.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
   <url><loc>https://example.com/item3</loc></url>
   <url><loc>https://example.com/item4</loc></url>
</urlset>`))
			gw.Close()
			w.Header().Set("Content-Type", "application/x-gzip")
			w.Write(gzBuf.Bytes())

		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewTestClient(true)
	urls, err := FetchAndParseSitemap(context.Background(), client, server.URL+"/sitemap_index.xml")
	if err != nil {
		t.Fatalf("FetchAndParseSitemap failed: %v", err)
	}

	if len(urls) != 4 {
		t.Fatalf("expected 4 recursive URLs, got %d: %v", len(urls), urls)
	}
}
