package sitemap

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"strings"
)

type URLSet struct {
	XMLName xml.Name `xml:"urlset"`
	URLs    []URL    `xml:"url"`
}

type URL struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod"`
}

type SitemapIndex struct {
	XMLName  xml.Name     `xml:"sitemapindex"`
	Sitemaps []SitemapLoc `xml:"sitemap"`
}

type SitemapLoc struct {
	Loc string `xml:"loc"`
}

const MaxSitemapSize = 50 * 1024 * 1024 // 50MB bounded limit

// ParseSitemapXML parses sitemap.xml bytes using a streaming decoder and returns canonical URLs or child sitemap URLs.
func ParseSitemapXML(xmlBytes []byte) ([]string, error) {
	if len(xmlBytes) == 0 {
		return nil, fmt.Errorf("empty sitemap xml")
	}
	return ParseSitemapReader(bytes.NewReader(xmlBytes))
}

// ParseSitemapReader parses sitemap XML from an io.Reader using a bounded streaming xml.Decoder.
func ParseSitemapReader(r io.Reader) ([]string, error) {
	if r == nil {
		return nil, fmt.Errorf("nil reader provided")
	}

	limited := io.LimitReader(r, MaxSitemapSize)
	decoder := xml.NewDecoder(limited)

	var urls []string
	var foundRoot bool
	var rootName string

	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("malformed sitemap XML: %w", err)
		}

		switch elem := token.(type) {
		case xml.StartElement:
			local := strings.ToLower(elem.Name.Local)
			if !foundRoot {
				foundRoot = true
				rootName = local
				if rootName != "urlset" && rootName != "sitemapindex" {
					return nil, fmt.Errorf("invalid sitemap root element '%s', expected 'urlset' or 'sitemapindex'", elem.Name.Local)
				}
				continue
			}

			if rootName == "urlset" && local == "url" {
				var u URL
				if err := decoder.DecodeElement(&u, &elem); err != nil {
					return nil, fmt.Errorf("failed to decode url element: %w", err)
				}
				cleanURL, ok := sanitizeURL(u.Loc)
				if ok {
					urls = append(urls, cleanURL)
				}
			} else if rootName == "sitemapindex" && local == "sitemap" {
				var s SitemapLoc
				if err := decoder.DecodeElement(&s, &elem); err != nil {
					return nil, fmt.Errorf("failed to decode sitemap element: %w", err)
				}
				cleanURL, ok := sanitizeURL(s.Loc)
				if ok {
					urls = append(urls, cleanURL)
				}
			}
		}
	}

	if !foundRoot {
		return nil, fmt.Errorf("empty or invalid sitemap XML: no root element found")
	}

	return urls, nil
}

func sanitizeURL(rawURL string) (string, bool) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return "", false
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", false
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", false
	}

	if parsed.Host == "" {
		return "", false
	}

	return parsed.String(), true
}
