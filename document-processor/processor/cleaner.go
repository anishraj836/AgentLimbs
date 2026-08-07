package processor

import (
	"github.com/crawler-monorepo/internal/extractor"
)

type CleanDocument = extractor.CleanDocument

// ProcessRawHTML cleans raw HTML, strips boilerplate, and extracts title & body text.
func ProcessRawHTML(sourceURL string, htmlContent []byte) (*CleanDocument, error) {
	return extractor.ProcessRawHTML(sourceURL, htmlContent)
}
