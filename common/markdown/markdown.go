package markdown

import (
	"bytes"
	"fmt"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// ConvertHTMLToMarkdown converts raw HTML into token-efficient Github-Flavored Markdown using clean_rag mode.
func ConvertHTMLToMarkdown(sourceURL string, htmlBytes []byte) (markdownText string, tokenEstimate int, title string) {
	return ConvertHTMLToMarkdownWithMode(sourceURL, htmlBytes, "clean_rag")
}

// ConvertHTMLToMarkdownWithMode supports custom extraction modes:
// - "clean_rag" (default): Strips sidebars/TOC and same-page anchor URLs for max token reduction.
// - "preserve_links": Preserves all full URL links (including fragment links).
// - "raw": Minimal filtering; preserves original DOM structure.
func ConvertHTMLToMarkdownWithMode(sourceURL string, htmlBytes []byte, mode string) (markdownText string, tokenEstimate int, title string) {
	if mode == "" {
		mode = "clean_rag"
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(htmlBytes))
	if err != nil {
		return string(htmlBytes), len(strings.Fields(string(htmlBytes))), sourceURL
	}

	// Remove non-content elements and navigational sidebars in clean_rag and preserve_links modes
	if mode != "raw" {
		doc.Find("script, style, noscript, iframe, nav, footer, header, form, svg, aside, .toc, .table-of-contents, .breadcrumb, .sidebar, .menu, .ad, .advertisement").Remove()
	} else {
		doc.Find("script, style, noscript").Remove()
	}

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

	// Process Tables
	doc.Find("table").Each(func(i int, tbl *goquery.Selection) {
		var headers []string
		tbl.Find("th").Each(func(j int, th *goquery.Selection) {
			headers = append(headers, strings.TrimSpace(th.Text()))
		})

		var rows [][]string
		tbl.Find("tr").Each(func(j int, tr *goquery.Selection) {
			var row []string
			tr.Find("td").Each(func(k int, td *goquery.Selection) {
				row = append(row, strings.TrimSpace(td.Text()))
			})
			if len(row) > 0 {
				rows = append(rows, row)
			}
		})

		if len(headers) == 0 && len(rows) > 0 {
			for colIdx := range rows[0] {
				headers = append(headers, fmt.Sprintf("Col %d", colIdx+1))
			}
		}

		if len(headers) > 0 {
			sb.WriteString("| " + strings.Join(headers, " | ") + " |\n")
			dividers := make([]string, len(headers))
			for d := range dividers {
				dividers[d] = "---"
			}
			sb.WriteString("| " + strings.Join(dividers, " | ") + " |\n")

			for _, r := range rows {
				sb.WriteString("| " + strings.Join(r, " | ") + " |\n")
			}
			sb.WriteString("\n")
		}
	})

	// Process Main Block Elements (only top-level blocks to prevent child node duplication)
	doc.Find("body").Find("h1, h2, h3, h4, h5, h6, p, ul, ol, blockquote, pre").Each(func(i int, s *goquery.Selection) {
		// Skip if nested inside an outer block container already handled
		if s.ParentsFiltered("p, ul, ol, blockquote, pre").Length() > 0 {
			return
		}

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
			formattedText := formatElementText(s, baseURL, mode)
			if formattedText != "" {
				sb.WriteString(fmt.Sprintf("%s\n\n", formattedText))
			}
		case "ul", "ol":
			s.Find("li").Each(func(j int, li *goquery.Selection) {
				itemText := formatElementText(li, baseURL, mode)
				if itemText != "" {
					sb.WriteString(fmt.Sprintf("- %s\n", itemText))
				}
			})
			sb.WriteString("\n")
		case "blockquote":
			sb.WriteString(fmt.Sprintf("> %s\n\n", text))
		case "pre":
			sb.WriteString(fmt.Sprintf("```\n%s\n```\n\n", text))
		}
	})

	result := strings.TrimSpace(sb.String())
	tokenEstimate = len(strings.Fields(result))

	return result, tokenEstimate, title
}

// formatElementText processes links and inline code elements directly within DOM nodes
func formatElementText(s *goquery.Selection, baseURL *url.URL, mode string) string {
	// Format inline code elements with backticks
	s.Find("code").Each(func(j int, code *goquery.Selection) {
		codeText := strings.TrimSpace(code.Text())
		if codeText != "" {
			code.SetText("`" + codeText + "`")
		}
	})

	// Format links directly in DOM node
	s.Find("a[href]").Each(func(j int, a *goquery.Selection) {
		href, ok := a.Attr("href")
		anchorText := strings.TrimSpace(a.Text())
		if ok && anchorText != "" {
			// Skip same-page anchor fragment links (e.g. #GOGC) ONLY in clean_rag mode
			if mode == "clean_rag" && strings.HasPrefix(href, "#") {
				a.SetText(anchorText)
				return
			}
			absURL := href
			if parsed, err := url.Parse(href); err == nil && baseURL != nil {
				absURL = baseURL.ResolveReference(parsed).String()
			}
			a.SetText(fmt.Sprintf("[%s](%s)", anchorText, absURL))
		}
	})

	return strings.TrimSpace(s.Text())
}
