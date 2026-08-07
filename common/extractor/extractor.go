package extractor

import (
	internalExtractor "github.com/crawler-monorepo/internal/extractor"
)

// ExtractFields extracts structured key-value pairs from Markdown text based on field schemas.
func ExtractFields(markdownText string, fields []string) map[string]string {
	return internalExtractor.ExtractFields(markdownText, fields)
}
