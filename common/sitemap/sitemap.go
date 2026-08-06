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

// ParseSitemapXML parses sitemap.xml bytes and returns all canonical URLs.
func ParseSitemapXML(xmlBytes []byte) ([]string, error) {
	var urlset URLSet
	if err := xml.Unmarshal(xmlBytes, &urlset); err != nil {
		return nil, err
	}

	var urls []string
	for _, u := range urlset.URLs {
		loc := strings.TrimSpace(u.Loc)
		if loc != "" {
			urls = append(urls, loc)
		}
	}

	return urls, nil
}
