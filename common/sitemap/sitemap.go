package sitemap

import (
	"encoding/xml"
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

// ParseSitemapXML parses sitemap.xml bytes and returns all canonical URLs or child sitemap URLs.
func ParseSitemapXML(xmlBytes []byte) ([]string, error) {
	var urlset URLSet
	if err := xml.Unmarshal(xmlBytes, &urlset); err == nil && len(urlset.URLs) > 0 {
		var urls []string
		for _, u := range urlset.URLs {
			loc := strings.TrimSpace(u.Loc)
			if loc != "" {
				urls = append(urls, loc)
			}
		}
		if len(urls) > 0 {
			return urls, nil
		}
	}

	var sitemapIndex SitemapIndex
	if err := xml.Unmarshal(xmlBytes, &sitemapIndex); err == nil && len(sitemapIndex.Sitemaps) > 0 {
		var urls []string
		for _, s := range sitemapIndex.Sitemaps {
			loc := strings.TrimSpace(s.Loc)
			if loc != "" {
				urls = append(urls, loc)
			}
		}
		if len(urls) > 0 {
			return urls, nil
		}
	}

	return nil, nil
}
