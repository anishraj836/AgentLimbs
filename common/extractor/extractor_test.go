package extractor

import (
	"testing"
)

func TestExtractFields(t *testing.T) {
	md := `# Go Programming Language

- Title: The Go Programming Language
- License: Open Source
- Author: Google
`

	fields := []string{"Title", "License"}
	extracted := ExtractFields(md, fields)

	if extracted["Title"] == "" || extracted["License"] == "" {
		t.Errorf("failed to extract structured fields: %v", extracted)
	}
}
