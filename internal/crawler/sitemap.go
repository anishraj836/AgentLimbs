package crawler

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

const (
	MaxSitemapURLs       = 50000
	MaxSitemapRecursion  = 2
	MaxSitemapFetchBytes = 25 * 1024 * 1024 // 25MB
)

type xmlURLSet struct {
	XMLName xml.Name `xml:"urlset"`
	URLs    []xmlURL `xml:"url"`
}

type xmlURL struct {
	Loc string `xml:"loc"`
}

type xmlSitemapIndex struct {
	XMLName  xml.Name        `xml:"sitemapindex"`
	Sitemaps []xmlSitemapLoc `xml:"sitemap"`
}

type xmlSitemapLoc struct {
	Loc string `xml:"loc"`
}

// ParseSitemapContent parses raw XML or gzipped XML and separates page URLs from child sitemap index URLs.
func ParseSitemapContent(data []byte) (pageURLs []string, childSitemaps []string, err error) {
	if len(data) == 0 {
		return nil, nil, fmt.Errorf("empty sitemap data")
	}

	var reader io.Reader = bytes.NewReader(data)
	// Transparent gzip detection via magic header (0x1f, 0x8b)
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		gzReader, gzErr := gzip.NewReader(bytes.NewReader(data))
		if gzErr != nil {
			return nil, nil, fmt.Errorf("failed to create gzip reader for sitemap: %w", gzErr)
		}
		defer gzReader.Close()
		reader = gzReader
	}

	limited := io.LimitReader(reader, MaxSitemapFetchBytes)
	decoder := xml.NewDecoder(limited)

	var foundRoot bool
	var rootLocal string

	for {
		token, tokenErr := decoder.Token()
		if tokenErr != nil {
			if tokenErr == io.EOF {
				break
			}
			return nil, nil, fmt.Errorf("malformed sitemap XML: %w", tokenErr)
		}

		switch elem := token.(type) {
		case xml.StartElement:
			local := strings.ToLower(elem.Name.Local)
			if !foundRoot {
				foundRoot = true
				rootLocal = local
				if rootLocal != "urlset" && rootLocal != "sitemapindex" {
					return nil, nil, fmt.Errorf("invalid sitemap root element '%s', expected 'urlset' or 'sitemapindex'", elem.Name.Local)
				}
				continue
			}

			if rootLocal == "urlset" && local == "url" {
				var u xmlURL
				if decErr := decoder.DecodeElement(&u, &elem); decErr != nil {
					return nil, nil, fmt.Errorf("failed to decode url element: %w", decErr)
				}
				loc := strings.TrimSpace(u.Loc)
				if isValidHTTPURL(loc) {
					pageURLs = append(pageURLs, loc)
				}
			} else if rootLocal == "sitemapindex" && local == "sitemap" {
				var s xmlSitemapLoc
				if decErr := decoder.DecodeElement(&s, &elem); decErr != nil {
					return nil, nil, fmt.Errorf("failed to decode sitemap element: %w", decErr)
				}
				loc := strings.TrimSpace(s.Loc)
				if isValidHTTPURL(loc) {
					childSitemaps = append(childSitemaps, loc)
				}
			}
		}
	}

	if !foundRoot {
		return nil, nil, fmt.Errorf("empty or invalid sitemap XML: no root element found")
	}

	return pageURLs, childSitemaps, nil
}

func isValidHTTPURL(raw string) bool {
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	return (scheme == "http" || scheme == "https") && u.Host != ""
}

// FetchAndParseSitemap recursively traverses sitemaps up to max recursion depth 2 and capped at 50k URLs.
func FetchAndParseSitemap(ctx context.Context, client *Client, sitemapURL string) ([]string, error) {
	if client == nil {
		client = NewClient()
	}

	var mu sync.Mutex
	collectedURLs := make([]string, 0)
	visitedSitemaps := make(map[string]bool)

	var parseRecursive func(currentURL string, currentDepth int) error
	parseRecursive = func(currentURL string, currentDepth int) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if currentDepth > MaxSitemapRecursion {
			return nil
		}

		mu.Lock()
		if visitedSitemaps[currentURL] || len(collectedURLs) >= MaxSitemapURLs {
			mu.Unlock()
			return nil
		}
		visitedSitemaps[currentURL] = true
		mu.Unlock()

		req, err := http.NewRequestWithContext(ctx, "GET", currentURL, nil)
		if err != nil {
			return err
		}
		ApplyAntiBotHeaders(req, GetRotatedHeaderProfile())
		req.Header.Set("Accept", "application/xml, text/xml, application/x-gzip, */*")

		resp, doErr := client.client.Do(req)
		if doErr != nil {
			return doErr
		}
		defer func() {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
			resp.Body.Close()
		}()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("HTTP %d fetching sitemap %s", resp.StatusCode, currentURL)
		}

		limitedBody := io.LimitReader(resp.Body, MaxSitemapFetchBytes)
		bodyBytes, readErr := io.ReadAll(limitedBody)
		if readErr != nil {
			return readErr
		}

		// Transparent gzip decompression if header or extension indicates gzip
		isGzip := strings.HasSuffix(strings.ToLower(currentURL), ".gz") ||
			strings.Contains(resp.Header.Get("Content-Type"), "gzip") ||
			strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") ||
			(len(bodyBytes) >= 2 && bodyBytes[0] == 0x1f && bodyBytes[1] == 0x8b)

		var pageURLs, childSitemaps []string
		var parseErr error

		if isGzip && !(len(bodyBytes) >= 2 && bodyBytes[0] == 0x1f && bodyBytes[1] == 0x8b) {
			gzReader, err := gzip.NewReader(bytes.NewReader(bodyBytes))
			if err == nil {
				defer gzReader.Close()
				decompressed, _ := io.ReadAll(gzReader)
				pageURLs, childSitemaps, parseErr = ParseSitemapContent(decompressed)
			} else {
				pageURLs, childSitemaps, parseErr = ParseSitemapContent(bodyBytes)
			}
		} else {
			pageURLs, childSitemaps, parseErr = ParseSitemapContent(bodyBytes)
		}

		if parseErr != nil {
			return parseErr
		}

		mu.Lock()
		for _, u := range pageURLs {
			if len(collectedURLs) >= MaxSitemapURLs {
				break
			}
			collectedURLs = append(collectedURLs, u)
		}
		mu.Unlock()

		// Recurse on child sitemaps if within depth budget
		if currentDepth < MaxSitemapRecursion {
			for _, childURL := range childSitemaps {
				mu.Lock()
				exceeded := len(collectedURLs) >= MaxSitemapURLs
				mu.Unlock()
				if exceeded {
					break
				}
				_ = parseRecursive(childURL, currentDepth+1)
			}
		}

		return nil
	}

	err := parseRecursive(sitemapURL, 0)
	if err != nil && len(collectedURLs) == 0 {
		return nil, err
	}

	return collectedURLs, nil
}

// DiscoverSitemaps returns potential sitemap locations for a root host.
func DiscoverSitemaps(seedURL string) []string {
	u, err := url.Parse(seedURL)
	if err != nil || u.Host == "" {
		return nil
	}

	baseURL := fmt.Sprintf("%s://%s", u.Scheme, u.Host)
	return []string{
		baseURL + "/sitemap.xml",
		baseURL + "/sitemap_index.xml",
		baseURL + "/sitemap.xml.gz",
	}
}
