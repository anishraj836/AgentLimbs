package markdown

import (
	"github.com/crawler-monorepo/internal/extractor"
)

// ConvertHTMLToMarkdown converts raw HTML into token-efficient Github-Flavored Markdown using clean_rag mode.
func ConvertHTMLToMarkdown(sourceURL string, htmlBytes []byte) (markdownText string, tokenEstimate int, title string) {
	return extractor.ConvertHTMLToMarkdown(sourceURL, htmlBytes, "clean_rag")
}

// ConvertHTMLToMarkdownWithMode supports custom extraction modes.
func ConvertHTMLToMarkdownWithMode(sourceURL string, htmlBytes []byte, mode string) (markdownText string, tokenEstimate int, title string) {
	return extractor.ConvertHTMLToMarkdown(sourceURL, htmlBytes, mode)
}

// CountBPETokens calculates exact Byte-Pair Encoding (BPE) subword tokens.
func CountBPETokens(text string) int {
	return extractor.CountBPETokens(text)
}
