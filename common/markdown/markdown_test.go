package markdown

import (
	"strings"
	"testing"
)

func TestConvertHTMLToMarkdown(t *testing.T) {
	html := []byte(`
		<html>
			<head><title>Test Page</title></head>
			<body>
				<h1>Welcome to Test Page</h1>
				<p>This is a paragraph with a <a href="/about">link</a>.</p>
				<ul>
					<li>Item 1</li>
					<li>Item 2</li>
				</ul>
			</body>
		</html>
	`)

	md, tokens, title := ConvertHTMLToMarkdown("https://example.com", html)
	if title != "Test Page" {
		t.Errorf("expected title 'Test Page', got %q", title)
	}

	if tokens <= 0 {
		t.Errorf("expected positive token estimate, got %d", tokens)
	}

	if !strings.Contains(md, "# Welcome to Test Page") {
		t.Errorf("expected Markdown to contain heading, got:\n%s", md)
	}

	if !strings.Contains(md, "[link](https://example.com/about)") {
		t.Errorf("expected Markdown to contain resolved link, got:\n%s", md)
	}
}
