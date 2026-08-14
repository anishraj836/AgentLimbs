package extractor

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestLiveArxivPaperExtraction(t *testing.T) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get("https://arxiv.org/pdf/1706.03762")
	if err != nil {
		t.Skipf("Network unavailable: %v", err)
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read body: %v", err)
	}

	streams := extractPDFStreams(body)
	t.Logf("TOTAL STREAMS: %d", len(streams))
	for idx, s := range streams {
		decomp := decompressPDFStream(s.dict, s.data)
		extracted := extractTextFromBTBlocks(decomp)
		joined := strings.Join(extracted, "\n")
		if strings.Contains(joined, "3.2.3") || strings.Contains(joined, "encoder-decoder attention") {
			t.Logf("STREAM %d CONTAINS 3.2.3 (dict: %s):\nRAW EXTRACTED:\n%s\n", idx, s.dict, joined)
		}
	}

	text, title, err := ExtractTextFromPDF(body)
	if err != nil {
		t.Fatalf("ExtractTextFromPDF failed: %v", err)
	}

	pos323 := strings.Index(text, "3.2.3")
	if pos323 != -1 {
		end := pos323 + 600
		if end > len(text) {
			end = len(text)
		}
		lines := strings.Split(text[pos323:end], "\n")
		for idx, l := range lines {
			t.Logf("LINE %d: %q (len=%d, runes=%v)", idx, l, len(l), []rune(l))
		}
	}
	// Check if title is Attention Is All You Need
	if title != "Attention Is All You Need" {
		t.Errorf("Expected title 'Attention Is All You Need', got %q", title)
	}

	// Check for "first" vs "rst"
	if strings.Contains(text, " rst ") || strings.Contains(text, "The rst ") || strings.Contains(text, "the rst ") {
		t.Errorf("Found 'rst' in extracted text: %s", findSnippets(text, "rst"))
	}

	// Check Section 3.2.3 bullets
	if !strings.Contains(text, "- In \"encoder-decoder attention\"") {
		t.Errorf("Expected bullet '- In \"encoder-decoder attention\"' in Section 3.2.3, text snippet:\n%s", findSnippets(text, "encoder-decoder attention"))
	}
	if !strings.Contains(text, "- The encoder contains self-attention") {
		t.Errorf("Expected bullet '- The encoder contains self-attention' in Section 3.2.3, text snippet:\n%s", findSnippets(text, "encoder contains"))
	}
	if !strings.Contains(text, "- Similarly, self-attention") {
		t.Errorf("Expected bullet '- Similarly, self-attention' in Section 3.2.3, text snippet:\n%s", findSnippets(text, "Similarly, self-attention"))
	}

	// Check information flow
	if !strings.Contains(text, "information flow") {
		t.Errorf("Expected 'information flow' to be repaired, text snippet:\n%s", findSnippets(text, "information"))
	}
}

func findSnippets(text, term string) string {
	var res []string
	lines := strings.Split(text, "\n")
	for _, l := range lines {
		if strings.Contains(l, term) {
			res = append(res, l)
			if len(res) >= 3 {
				break
			}
		}
	}
	return strings.Join(res, " | ")
}
