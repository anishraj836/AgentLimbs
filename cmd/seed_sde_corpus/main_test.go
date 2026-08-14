package main

import (
	"strings"
	"testing"
)

func TestGenerateArticleMarkdown(t *testing.T) {
	title, body := generateArticleMarkdown("Data Structures & Algorithms", "Red-Black Tree Self Balancing", 1)

	if !strings.Contains(title, "Data Structures & Algorithms: Red-Black Tree Self Balancing") {
		t.Errorf("Unexpected title: %s", title)
	}

	if !strings.Contains(body, "## Architectural Overview") {
		t.Errorf("Expected Architectural Overview header in markdown body")
	}

	if !strings.Contains(body, "```go") {
		t.Errorf("Expected Go code block in markdown body")
	}

	if !strings.Contains(body, "## Operational Trade-Offs & Complexity Analysis") {
		t.Errorf("Expected Complexity Analysis header in markdown body")
	}
}

func TestSlugify(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"Data Structures & Algorithms", "Data-Structures---Algorithms"},
		{"Red-Black Tree Self Balancing", "Red-Black-Tree-Self-Balancing"},
		{"HTTP/2 & HTTP/3", "HTTP2---HTTP3"},
	}

	for _, tc := range testCases {
		res := slugify(tc.input)
		if res != tc.expected {
			t.Errorf("slugify(%q) = %q, expected %q", tc.input, res, tc.expected)
		}
	}
}
