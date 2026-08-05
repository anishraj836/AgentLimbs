package markdown

import (
	"bytes"
	"fmt"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// ConvertHTMLToMarkdown converts raw HTML into token-efficient, LLM-ready Github-Flavored Markdown.
func ConvertHTMLToMarkdown(sourceURL string, htmlBytes []byte) (markdownText string, tokenEstimate int, title string) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(htmlBytes))
	if err != nil {
		return string(htmlBytes), len(strings.Fields(string(htmlBytes))), sourceURL
	}

	// Remove non-content elements
	doc.Find("script, style, noscript, iframe, nav, footer, header, form, svg, aside, style").Remove()

	title = strings.TrimSpace(doc.Find("title").Text())
	if title == "" {
		title = strings.TrimSpace(doc.Find("h1").First().Text())
	}
	if title == "" {
		title = sourceURL
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# %s\n\n", title))

	baseURL, _ := url.Parse(sourceURL)

	doc.Find("body").Find("h1, h2, h3, h4, h5, h6, p, ul, ol, blockquote, pre, code").Each(func(i int, s *goquery.Selection) {
		tagName := goquery.NodeName(s)
		text := strings.TrimSpace(s.Text())

		if text == "" {
			return
		}

		switch tagName {
		case "h1":
			sb.WriteString(fmt.Sprintf("# %s\n\n", text))
		case "h2":
			sb.WriteString(fmt.Sprintf("## %s\n\n", text))
		case "h3":
			sb.WriteString(fmt.Sprintf("### %s\n\n", text))
		case "h4", "h5", "h6":
			sb.WriteString(fmt.Sprintf("#### %s\n\n", text))
		case "p":
			// Check for links inside paragraph
			linkText := text
			s.Find("a[href]").Each(func(j int, a *goquery.Selection) {
				href, ok := a.Attr("href")
				anchorText := strings.TrimSpace(a.Text())
				if ok && anchorText != "" {
					if parsed, err := url.Parse(href); err == nil && baseURL != nil {
						abs := baseURL.ResolveReference(parsed).String()
						linkText = strings.Replace(linkText, anchorText, fmt.Sprintf("[%s](%s)", anchorText, abs), 1)
					}
				}
			})
			sb.WriteString(fmt.Sprintf("%s\n\n", linkText))
		case "ul", "ol":
			s.Find("li").Each(func(j int, li *goquery.Selection) {
				itemText := strings.TrimSpace(li.Text())
				if itemText != "" {
					sb.WriteString(fmt.Sprintf("- %s\n", itemText))
				}
			})
			sb.WriteString("\n")
		case "blockquote":
			sb.WriteString(fmt.Sprintf("> %s\n\n", text))
		case "pre", "code":
			sb.WriteString(fmt.Sprintf("```\n%s\n```\n\n", text))
		}
	})

	result := strings.TrimSpace(sb.String())
	tokenEstimate = len(strings.Fields(result))

	return result, tokenEstimate, title
}
