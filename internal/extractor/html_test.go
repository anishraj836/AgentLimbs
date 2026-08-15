package extractor

import (
	"strings"
	"testing"
)

func TestConvertHTMLToMarkdown(t *testing.T) {
	htmlInput := `<!DOCTYPE html>
<html>
<head>
    <title>Test Page</title>
    <script>console.log("ignore me");</script>
    <style>body { color: red; }</style>
</head>
<body>
    <header><h1>Nav Header</h1></header>
    <nav><a href="/home">Home</a></nav>
    <main>
        <h1>Main Title</h1>
        <p>This is a paragraph with a <a href="https://example.com/link">link</a> and <code>inline code</code>.</p>
        <h2>Section 2</h2>
        <ul>
            <li>First item</li>
            <li>Second item</li>
        </ul>
        <pre>func main() {}</pre>
        <iframe>http://example.com/frame</iframe>
    </main>
    <footer>Copyright 2026</footer>
</body>
</html>`

	md, tokens, title := ConvertHTMLToMarkdown("https://example.com", []byte(htmlInput), "clean_rag")

	if title != "Test Page" {
		t.Errorf("Expected title 'Test Page', got '%s'", title)
	}

	if tokens <= 0 {
		t.Errorf("Expected positive token count, got %d", tokens)
	}

	// Verify ignored non-content nodes
	if strings.Contains(md, "console.log") {
		t.Errorf("Expected script tag to be ignored")
	}
	if strings.Contains(md, "color: red") {
		t.Errorf("Expected style tag to be ignored")
	}
	if strings.Contains(md, "Nav Header") {
		t.Errorf("Expected header tag to be ignored")
	}
	if strings.Contains(md, "Copyright 2026") {
		t.Errorf("Expected footer tag to be ignored")
	}
	if strings.Contains(md, "http://example.com/frame") {
		t.Errorf("Expected iframe tag to be ignored")
	}

	// Verify converted elements
	if !strings.Contains(md, "# Main Title") {
		t.Errorf("Expected '# Main Title' in markdown")
	}
	if !strings.Contains(md, "## Section 2") {
		t.Errorf("Expected '## Section 2' in markdown")
	}
	if !strings.Contains(md, "[link](https://example.com/link)") {
		t.Errorf("Expected link markdown in output, got:\n%s", md)
	}
	if !strings.Contains(md, "`inline code`") {
		t.Errorf("Expected inline code in markdown")
	}
	if !strings.Contains(md, "- First item") {
		t.Errorf("Expected '- First item' list element")
	}
	if !strings.Contains(md, "```\nfunc main() {}\n```") {
		t.Errorf("Expected pre code block in markdown")
	}
}

func TestProcessRawHTML(t *testing.T) {
	htmlInput := `<html lang="en">
<head>
    <title>Doc Title</title>
    <meta name="pubdate" content="2026-08-08">
</head>
<body>
    <p>Sample body text here.</p>
    <a href="/page2">Link to Page 2</a>
</body>
</html>`

	doc, err := ProcessRawHTML("https://example.com/base", []byte(htmlInput))
	if err != nil {
		t.Fatalf("ProcessRawHTML failed: %v", err)
	}

	if doc.Title != "Doc Title" {
		t.Errorf("Expected title 'Doc Title', got '%s'", doc.Title)
	}
	if doc.Language != "en" {
		t.Errorf("Expected lang 'en', got '%s'", doc.Language)
	}
	if doc.Timestamp != "2026-08-08" {
		t.Errorf("Expected pubdate '2026-08-08', got '%s'", doc.Timestamp)
	}
	if len(doc.Links) != 1 || doc.Links[0] != "https://example.com/page2" {
		t.Errorf("Expected resolved link 'https://example.com/page2', got %v", doc.Links)
	}
	if !strings.Contains(doc.Body, "Sample body text here.") {
		t.Errorf("Expected body content in CleanDocument, got '%s'", doc.Body)
	}
}

func TestExtractFields(t *testing.T) {
	mdText := "# Company Name\nACME Corp\n\n- Revenue: $1,000,000"
	extracted := ExtractFields(mdText, []string{"Revenue", "Company"})

	if extracted["Revenue"] != "Revenue: $1,000,000" {
		t.Errorf("Expected Revenue field extraction, got '%s'", extracted["Revenue"])
	}
}

func TestCountBPETokens_SpecialTokens(t *testing.T) {
	specialTexts := []string{
		"Hello <|endoftext|> world",
		"<|im_start|>system\nYou are an AI assistant.<|im_end|>",
		"Code with <|fim_prefix|> prefix <|fim_suffix|> suffix <|fim_middle|> middle",
		"",
	}

	for _, text := range specialTexts {
		// Should count tokens without panicking
		tokens := CountBPETokens(text)
		if text != "" && tokens <= 0 {
			t.Errorf("Expected positive token count for %q, got %d", text, tokens)
		}
	}
}

func TestConvertHTMLToMarkdown_NestedTables(t *testing.T) {
	htmlInput := `<!DOCTYPE html>
<html>
<head><title>Nested Table Page</title></head>
<body>
    <table id="outer">
        <thead>
            <tr><th>OuterCol1</th><th>OuterCol2</th></tr>
        </thead>
        <tbody>
            <tr>
                <td>OuterVal1</td>
                <td>
                    <table id="inner">
                        <tr><th>InnerColA</th><th>InnerColB</th></tr>
                        <tr><td>InnerValA</td><td>InnerValB</td></tr>
                    </table>
                </td>
            </tr>
            <tr>
                <td>OuterVal2</td>
                <td>OuterVal3</td>
            </tr>
        </tbody>
    </table>
</body>
</html>`

	md, _, _ := ConvertHTMLToMarkdown("https://example.com", []byte(htmlInput), "clean_rag")

	if !strings.Contains(md, "| OuterCol1 | OuterCol2 |") {
		t.Errorf("Expected outer table header row '| OuterCol1 | OuterCol2 |', got:\n%s", md)
	}

	if !strings.Contains(md, "| InnerColA | InnerColB |") {
		t.Errorf("Expected inner table header row '| InnerColA | InnerColB |', got:\n%s", md)
	}

	if strings.Contains(md, "| OuterCol1 | OuterCol2 | InnerColA |") {
		t.Errorf("Outer table headers corrupted with inner headers:\n%s", md)
	}
}

