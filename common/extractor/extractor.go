package extractor

import (
	"strings"
)

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
