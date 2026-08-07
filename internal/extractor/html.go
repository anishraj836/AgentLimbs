package extractor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	tiktoken "github.com/pkoukk/tiktoken-go"
	"golang.org/x/net/html"
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

// ConvertHTMLToMarkdown converts raw HTML AST into token-efficient Markdown using mode ("clean_rag", "preserve_links", "raw").
func ConvertHTMLToMarkdown(sourceURL string, htmlBytes []byte, mode string) (markdownText string, tokenEstimate int, title string) {
	if mode == "" {
		mode = "clean_rag"
	}

	doc, err := html.Parse(bytes.NewReader(htmlBytes))
	if err != nil {
		return string(htmlBytes), CountBPETokens(string(htmlBytes)), sourceURL
	}

	title = ExtractTitleFromAST(doc)
	if title == "" {
		title = sourceURL
	}

	var baseURL *url.URL
	if sourceURL != "" {
		baseURL, _ = url.Parse(sourceURL)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# %s\n\n", title))

	processASTTables(doc, &sb)
	renderASTBody(doc, &sb, baseURL, mode)

	result := strings.TrimSpace(sb.String())
	tokenEstimate = CountBPETokens(result)
	return result, tokenEstimate, title
}

// ExtractTitleFromAST extracts document title from <title> or <h1> tag using AST traversal.
func ExtractTitleFromAST(n *html.Node) string {
	if n == nil {
		return ""
	}
	var title string
	var findTitle func(*html.Node)
	findTitle = func(node *html.Node) {
		if node.Type == html.ElementNode && strings.ToLower(node.Data) == "title" {
			title = strings.TrimSpace(getNodeText(node))
			return
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			findTitle(c)
			if title != "" {
				return
			}
		}
	}
	findTitle(n)

	if title == "" {
		var findH1 func(*html.Node)
		findH1 = func(node *html.Node) {
			if node.Type == html.ElementNode && strings.ToLower(node.Data) == "h1" {
				title = strings.TrimSpace(getNodeText(node))
				return
			}
			for c := node.FirstChild; c != nil; c = c.NextSibling {
				findH1(c)
				if title != "" {
					return
				}
			}
		}
		findH1(n)
	}

	return title
}

// CountBPETokens calculates exact Byte-Pair Encoding subword tokens using tiktoken.
func CountBPETokens(text string) int {
	if text == "" {
		return 0
	}
	tke, err := tiktoken.GetEncoding("cl100k_base")
	if err != nil {
		return len(strings.Fields(text))
	}
	tokens := tke.Encode(text, nil, nil)
	return len(tokens)
}

// ProcessRawHTML parses HTML DOM AST, removes non-content nodes, and returns a CleanDocument.
func ProcessRawHTML(sourceURL string, htmlContent []byte) (*CleanDocument, error) {
	doc, err := html.Parse(bytes.NewReader(htmlContent))
	if err != nil {
		return nil, err
	}

	title := ExtractTitleFromAST(doc)
	lang := extractAttributeFromAST(doc, "html", "lang")
	if lang == "" {
		lang = "en"
	}

	pubdate := extractMetaContentFromAST(doc, "pubdate")
	links := extractLinksFromAST(doc, sourceURL)

	bodyParts := extractBlockTextsFromAST(doc)
	cleanBody := strings.Join(bodyParts, " ")

	return &CleanDocument{
		URL:       sourceURL,
		Title:     title,
		Body:      cleanBody,
		Language:  lang,
		Timestamp: pubdate,
		Links:     links,
	}, nil
}

// SerializeJSON marshals CleanDocument to JSON.
func (c *CleanDocument) SerializeJSON() ([]byte, error) {
	return json.Marshal(c)
}

// ExtractFields extracts structured key-value pairs from Markdown text based on field schemas.
func ExtractFields(markdownText string, fields []string) map[string]string {
	result := make(map[string]string)
	lines := strings.Split(markdownText, "\n")

	for _, field := range fields {
		fLower := strings.ToLower(field)
		for _, line := range lines {
			lineLower := strings.ToLower(line)
			if strings.Contains(lineLower, fLower) {
				cleanLine := strings.TrimSpace(line)
				cleanLine = strings.TrimPrefix(cleanLine, "#")
				cleanLine = strings.TrimPrefix(cleanLine, "-")
				cleanLine = strings.TrimSpace(cleanLine)
				result[field] = cleanLine
				break
			}
		}
	}

	return result
}

// Helper functions for AST Traversal

func isIgnoredNode(n *html.Node, mode string) bool {
	if n == nil || n.Type != html.ElementNode {
		return false
	}
	tag := strings.ToLower(n.Data)
	ignored := map[string]bool{
		"script": true, "style": true, "noscript": true, "iframe": true,
		"nav": true, "footer": true, "header": true,
	}
	if mode != "raw" {
		ignored["form"] = true
		ignored["svg"] = true
		ignored["aside"] = true
	}
	return ignored[tag]
}

func getNodeText(n *html.Node) string {
	if n == nil {
		return ""
	}
	if n.Type == html.TextNode {
		return n.Data
	}
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		sb.WriteString(getNodeText(c))
	}
	return sb.String()
}

func extractTextFromAST(n *html.Node, mode string) string {
	if n == nil || isIgnoredNode(n, mode) {
		return ""
	}
	if n.Type == html.TextNode {
		return n.Data
	}
	var parts []string
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		t := strings.TrimSpace(extractTextFromAST(c, mode))
		if t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, " ")
}

func formatASTInlineText(n *html.Node, baseURL *url.URL, mode string) string {
	if n == nil {
		return ""
	}
	if n.Type == html.TextNode {
		return n.Data
	}
	if isIgnoredNode(n, mode) {
		return ""
	}

	tag := strings.ToLower(n.Data)
	if tag == "code" {
		t := strings.TrimSpace(getNodeText(n))
		if t != "" {
			return fmt.Sprintf("`%s`", t)
		}
		return ""
	}

	if tag == "a" {
		var href string
		for _, attr := range n.Attr {
			if strings.ToLower(attr.Key) == "href" {
				href = attr.Val
				break
			}
		}
		anchorText := strings.TrimSpace(getNodeText(n))
		if href != "" && anchorText != "" {
			if strings.HasPrefix(anchorText, "[") && strings.Contains(anchorText, "](") {
				return anchorText
			}
			if mode == "clean_rag" && strings.HasPrefix(href, "#") {
				return anchorText
			}
			absURL := href
			if parsed, err := url.Parse(href); err == nil && baseURL != nil {
				absURL = baseURL.ResolveReference(parsed).String()
			}
			return fmt.Sprintf("[%s](%s)", anchorText, absURL)
		}
		return anchorText
	}

	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		sb.WriteString(formatASTInlineText(c, baseURL, mode))
	}
	return sb.String()
}

func renderASTBody(n *html.Node, sb *strings.Builder, baseURL *url.URL, mode string) {
	if n == nil {
		return
	}

	if n.Type == html.ElementNode {
		if isIgnoredNode(n, mode) {
			return
		}

		tag := strings.ToLower(n.Data)
		switch tag {
		case "h1":
			t := strings.TrimSpace(getNodeText(n))
			if t != "" {
				sb.WriteString(fmt.Sprintf("# %s\n\n", t))
			}
			return
		case "h2":
			t := strings.TrimSpace(getNodeText(n))
			if t != "" {
				sb.WriteString(fmt.Sprintf("## %s\n\n", t))
			}
			return
		case "h3":
			t := strings.TrimSpace(getNodeText(n))
			if t != "" {
				sb.WriteString(fmt.Sprintf("### %s\n\n", t))
			}
			return
		case "h4", "h5", "h6":
			t := strings.TrimSpace(getNodeText(n))
			if t != "" {
				sb.WriteString(fmt.Sprintf("#### %s\n\n", t))
			}
			return
		case "p":
			t := strings.TrimSpace(formatASTInlineText(n, baseURL, mode))
			if t != "" {
				sb.WriteString(fmt.Sprintf("%s\n\n", t))
			}
			return
		case "ul", "ol":
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.ElementNode && strings.ToLower(c.Data) == "li" {
					itemText := strings.TrimSpace(formatASTInlineText(c, baseURL, mode))
					if itemText != "" {
						sb.WriteString(fmt.Sprintf("- %s\n", itemText))
					}
				}
			}
			sb.WriteString("\n")
			return
		case "blockquote":
			t := strings.TrimSpace(getNodeText(n))
			if t != "" {
				sb.WriteString(fmt.Sprintf("> %s\n\n", t))
			}
			return
		case "pre":
			t := strings.TrimSpace(getNodeText(n))
			if t != "" {
				sb.WriteString(fmt.Sprintf("```\n%s\n```\n\n", t))
			}
			return
		}
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		renderASTBody(c, sb, baseURL, mode)
	}
}

func processASTTables(n *html.Node, sb *strings.Builder) {
	if n == nil {
		return
	}
	if n.Type == html.ElementNode && strings.ToLower(n.Data) == "table" {
		var headers []string
		var rows [][]string

		var extractRows func(*html.Node)
		extractRows = func(node *html.Node) {
			if node.Type == html.ElementNode {
				tag := strings.ToLower(node.Data)
				if tag == "th" {
					headers = append(headers, strings.TrimSpace(getNodeText(node)))
				} else if tag == "tr" {
					var row []string
					for c := node.FirstChild; c != nil; c = c.NextSibling {
						if c.Type == html.ElementNode && strings.ToLower(c.Data) == "td" {
							row = append(row, strings.TrimSpace(getNodeText(c)))
						}
					}
					if len(row) > 0 {
						rows = append(rows, row)
					}
				}
			}
			for c := node.FirstChild; c != nil; c = c.NextSibling {
				extractRows(c)
			}
		}

		extractRows(n)

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
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		processASTTables(c, sb)
	}
}

func extractAttributeFromAST(n *html.Node, targetTag, attrKey string) string {
	if n == nil {
		return ""
	}
	if n.Type == html.ElementNode && strings.ToLower(n.Data) == strings.ToLower(targetTag) {
		for _, a := range n.Attr {
			if strings.ToLower(a.Key) == strings.ToLower(attrKey) {
				return a.Val
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		val := extractAttributeFromAST(c, targetTag, attrKey)
		if val != "" {
			return val
		}
	}
	return ""
}

func extractMetaContentFromAST(n *html.Node, name string) string {
	if n == nil {
		return ""
	}
	if n.Type == html.ElementNode && strings.ToLower(n.Data) == "meta" {
		var matchedName bool
		var content string
		for _, a := range n.Attr {
			if strings.ToLower(a.Key) == "name" && strings.ToLower(a.Val) == strings.ToLower(name) {
				matchedName = true
			}
			if strings.ToLower(a.Key) == "content" {
				content = a.Val
			}
		}
		if matchedName {
			return content
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		val := extractMetaContentFromAST(c, name)
		if val != "" {
			return val
		}
	}
	return ""
}

func extractLinksFromAST(n *html.Node, sourceURL string) []string {
	var links []string
	baseURL, _ := url.Parse(sourceURL)

	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && strings.ToLower(node.Data) == "a" {
			for _, a := range node.Attr {
				if strings.ToLower(a.Key) == "href" {
					if parsed, err := url.Parse(a.Val); err == nil && baseURL != nil {
						parsed.Fragment = ""
						abs := baseURL.ResolveReference(parsed).String()
						links = append(links, abs)
					}
					break
				}
			}
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return links
}

func extractBlockTextsFromAST(n *html.Node) []string {
	var parts []string
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node == nil {
			return
		}
		if isIgnoredNode(node, "clean_rag") {
			return
		}
		if node.Type == html.ElementNode {
			tag := strings.ToLower(node.Data)
			blockTags := map[string]bool{
				"p": true, "h1": true, "h2": true, "h3": true, "h4": true,
				"h5": true, "h6": true, "li": true, "article": true, "main": true, "section": true,
			}
			if blockTags[tag] {
				t := strings.TrimSpace(getNodeText(node))
				if len(t) > 3 {
					parts = append(parts, t)
				}
				return
			}
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return parts
}
