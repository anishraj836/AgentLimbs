package parser

import (
	"bytes"
	"net/url"

	"github.com/PuerkitoBio/goquery"
	"github.com/crawler-monorepo/common/logger"
	"github.com/crawler-monorepo/common/utils"
	"go.uber.org/zap"
)

// ExtractLinks parses the HTML and returns absolute URLs
func ExtractLinks(baseURI string, htmlContent []byte) ([]string, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(htmlContent))
	if err != nil {
		return nil, err
	}

	normalizedBase, err := utils.NormalizeURL(baseURI)
	if err != nil {
		return nil, err
	}

	baseURL, err := url.Parse(normalizedBase)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var links []string
	doc.Find("a[href]").Each(func(i int, s *goquery.Selection) {
		href, exists := s.Attr("href")
		if exists {
			// Resolve relative URLs to absolute
			parsedHref, err := url.Parse(href)
			if err == nil {
				absoluteURL := baseURL.ResolveReference(parsedHref).String()
				// Normalize after resolving so fragments, trailing slashes, and
				// unsupported schemes are handled consistently with the frontier.
				normalizedURL, err := utils.NormalizeURL(absoluteURL)
				if err != nil {
					return
				}

				// Exclude self-references and duplicate links.
				if normalizedURL != normalizedBase && !seen[normalizedURL] {
					seen[normalizedURL] = true
					links = append(links, normalizedURL)
				}
			}
		}
	})

	logger.Log.Debug("Extracted links from DOM", zap.Int("count", len(links)), zap.String("source", baseURI))
	return links, nil
}
