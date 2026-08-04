package processor

import (
	"bytes"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// CleanDocument represents a structured document after HTML boilerplate removal.
type CleanDocument struct {
	URL       string   `json:"url"`
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	Language  string   `json:"language"`
	Timestamp string   `json:"timestamp"`
	Links     []string `json:"links"`
}

// ProcessRawHTML cleans raw HTML, strips script/style/nav/ad boilerplate, and extracts title & body text.
func ProcessRawHTML(sourceURL string, htmlContent []byte) (*CleanDocument, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(htmlContent))
	if err != nil {
		return nil, err
	}

	// Remove non-content boilerplate elements
	doc.Find("script, style, noscript, iframe, nav, footer, header, form, svg, aside").Remove()

	// Extract Title
	title := strings.TrimSpace(doc.Find("title").Text())
	if title == "" {
		title = strings.TrimSpace(doc.Find("h1").First().Text())
	}

	// Extract Language
	lang, exists := doc.Find("html").Attr("lang")
	if !exists || lang == "" {
		lang = "en"
	}

	// Extract Outbound Links
	var links []string
	baseURL, _ := url.Parse(sourceURL)
	doc.Find("a[href]").Each(func(i int, s *goquery.Selection) {
		if href, ok := s.Attr("href"); ok {
			if parsed, err := url.Parse(href); err == nil && baseURL != nil {
				parsed.Fragment = ""
				abs := baseURL.ResolveReference(parsed).String()
				links = append(links, abs)
			}
		}
	})

	// Extract Body Text
	var textParts []string
	doc.Find("body").Find("p, h1, h2, h3, h4, h5, h6, li, article, main, section").Each(func(i int, s *goquery.Selection) {
		t := strings.TrimSpace(s.Text())
		if len(t) > 3 {
			textParts = append(textParts, t)
		}
	})

	cleanBody := strings.Join(textParts, " ")

	return &CleanDocument{
		URL:       sourceURL,
		Title:     title,
		Body:      cleanBody,
		Language:  lang,
		Timestamp: strings.TrimSpace(doc.Find("meta[name='pubdate']").AttrOr("content", "")),
		Links:     links,
	}, nil
}

// SerializeJSON marshals CleanDocument to JSON bytes.
func (c *CleanDocument) SerializeJSON() ([]byte, error) {
	return json.Marshal(c)
}
