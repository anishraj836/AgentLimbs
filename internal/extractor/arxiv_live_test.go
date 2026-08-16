package extractor

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func shouldSkipLiveTest(t *testing.T) bool {
	if os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != "" {
		t.Skip("Skipping live external network test in CI environment")
		return true
	}
	return false
}

func TestLiveArxivPaperExtraction(t *testing.T) {
	if shouldSkipLiveTest(t) {
		return
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get("https://arxiv.org/pdf/1706.03762")
	if err != nil {
		t.Skipf("Network unavailable: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Skipf("arxiv.org returned HTTP %d (rate-limited/blocked), skipping live test", resp.StatusCode)
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read body: %v", err)
	}

	text, title, err := ExtractTextFromPDF(body)
	if err != nil {
		t.Fatalf("ExtractTextFromPDF failed: %v", err)
	}

	// 1. Check title
	if title != "Attention Is All You Need" {
		t.Errorf("Expected title 'Attention Is All You Need', got %q", title)
	}

	// 2. Check for "first" vs "rst"
	if strings.Contains(text, " rst ") || strings.Contains(text, "The rst ") || strings.Contains(text, "the rst ") {
		t.Errorf("Found 'rst' in extracted text: %s", findSnippets(text, "rst"))
	}

	// 3. Check for "beneficial" vs "benecial"
	if strings.Contains(text, "benecial") {
		t.Errorf("Found un-repaired 'benecial' in extracted text: %s", findSnippets(text, "benecial"))
	}
	if !strings.Contains(text, "beneficial") {
		t.Errorf("Expected 'beneficial' in extracted text")
	}

	// 4. Check page range separators (en-dash)
	if !strings.Contains(text, "770–778") {
		t.Errorf("Expected en-dash in 770–778, snippet: %s", findSnippets(text, "770"))
	}
	if !strings.Contains(text, "1735–1780") {
		t.Errorf("Expected en-dash in 1735–1780, snippet: %s", findSnippets(text, "1735"))
	}
	if !strings.Contains(text, "832–841") {
		t.Errorf("Expected en-dash in 832–841, snippet: %s", findSnippets(text, "832"))
	}
	if !strings.Contains(text, "152–159") {
		t.Errorf("Expected en-dash in 152–159, snippet: %s", findSnippets(text, "152"))
	}
	if !strings.Contains(text, "433–440") {
		t.Errorf("Expected en-dash in 433–440, snippet: %s", findSnippets(text, "433"))
	}
	if !strings.Contains(text, "- In \"encoder-decoder attention\"") {
		t.Errorf("Expected bullet '- In \"encoder-decoder attention\"' in Section 3.2.3, text snippet:\n%s", findSnippets(text, "encoder-decoder attention"))
	}
	if !strings.Contains(text, "- The encoder contains self-attention") {
		t.Errorf("Expected bullet '- The encoder contains self-attention' in Section 3.2.3, text snippet:\n%s", findSnippets(text, "encoder contains"))
	}
	if !strings.Contains(text, "- Similarly, self-attention") {
		t.Errorf("Expected bullet '- Similarly, self-attention' in Section 3.2.3, text snippet:\n%s", findSnippets(text, "Similarly, self-attention"))
	}

	// 6. Check information flow
	if !strings.Contains(text, "information flow") {
		t.Errorf("Expected 'information flow' to be repaired, text snippet:\n%s", findSnippets(text, "information"))
	}
}

func TestLiveWord2VecPaperExtraction(t *testing.T) {
	if shouldSkipLiveTest(t) {
		return
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get("https://arxiv.org/pdf/1301.3781")
	if err != nil {
		t.Skipf("Network unavailable: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Skipf("arxiv.org returned HTTP %d (rate-limited/blocked), skipping live test", resp.StatusCode)
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read body: %v", err)
	}

	start := time.Now()
	text, title, err := ExtractTextFromPDF(body)
	elapsed := time.Since(start)
	t.Logf("Word2Vec Extraction took: %v (words=%d, title=%q)", elapsed, len(strings.Fields(text)), title)

	if err != nil {
		t.Fatalf("ExtractTextFromPDF failed: %v", err)
	}

	if title != "Efficient Estimation of Word Representations in Vector Space" {
		t.Errorf("Expected title 'Efficient Estimation of Word Representations in Vector Space', got %q", title)
	}

	// Check ligatures in Word2Vec
	badWords := []string{"simplifify", "inectional", " nd words", "dene ", "Unied", "inective", "higly"}
	for _, bad := range badWords {
		if strings.Contains(strings.ToLower(text), bad) {
			t.Errorf("Found ligature/spelling bug %q in Word2Vec: %s", bad, findSnippets(text, bad))
		}
	}
}

func TestLiveResNetPaperExtraction(t *testing.T) {
	if shouldSkipLiveTest(t) {
		return
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get("https://arxiv.org/pdf/1512.03385")
	if err != nil {
		t.Skipf("Network unavailable: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Skipf("arxiv.org returned HTTP %d (rate-limited/blocked), skipping live test", resp.StatusCode)
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read body: %v", err)
	}

	start := time.Now()
	text, title, err := ExtractTextFromPDF(body)
	elapsed := time.Since(start)
	t.Logf("ResNet Extraction took: %v (words=%d, title=%q)", elapsed, len(strings.Fields(text)), title)

	if err != nil {
		t.Fatalf("ExtractTextFromPDF failed: %v", err)
	}

	if !strings.Contains(title, "Deep Residual Learning") {
		t.Errorf("Expected title containing 'Deep Residual Learning', got %q", title)
	}
}

func TestLiveBERTPaperExtraction(t *testing.T) {
	if shouldSkipLiveTest(t) {
		return
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get("https://arxiv.org/pdf/1810.04805")
	if err != nil {
		t.Skipf("Network unavailable: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Skipf("arxiv.org returned HTTP %d (rate-limited/blocked), skipping live test", resp.StatusCode)
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read body: %v", err)
	}

	start := time.Now()
	text, title, err := ExtractTextFromPDF(body)
	elapsed := time.Since(start)
	t.Logf("BERT Extraction took: %v (words=%d, title=%q)", elapsed, len(strings.Fields(text)), title)

	if err != nil {
		t.Fatalf("ExtractTextFromPDF failed: %v", err)
	}

	if !strings.Contains(title, "BERT: Pre-training") {
		t.Errorf("Expected title containing 'BERT: Pre-training', got %q", title)
	}
}

func findSnippets(text, term string) string {
	var res []string
	lines := strings.Split(text, "\n")
	for _, l := range lines {
		if strings.Contains(strings.ToLower(l), strings.ToLower(term)) {
			res = append(res, l)
			if len(res) >= 3 {
				break
			}
		}
	}
	return strings.Join(res, " | ")
}
