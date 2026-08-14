package extractor

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"strings"
	"testing"
)

func TestIsPDF(t *testing.T) {
	pdfData := []byte("%PDF-1.4\n1 0 obj\n<<>>\nendobj")
	if !IsPDF(pdfData) {
		t.Errorf("Expected IsPDF to be true for valid PDF header")
	}

	htmlData := []byte("<html><body>Hello World</body></html>")
	if IsPDF(htmlData) {
		t.Errorf("Expected IsPDF to be false for HTML content")
	}

	if !IsPDFContent(htmlData, "application/pdf") {
		t.Errorf("Expected IsPDFContent to be true when Content-Type is application/pdf")
	}
}

func TestExtractTextFromPDF_Success(t *testing.T) {
	// Build a minimal uncompressed PDF stream containing > 15 words
	streamText := "BT /F1 12 Tf (This is a test PDF document created specifically for testing PDF text extraction. It contains well over fifteen words to pass the minimum word count check easily.) Tj ET"
	pdfRaw := fmt.Sprintf("%%PDF-1.4\n1 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\ntrailer\n<< /Root 1 0 R >>\n%%%%EOF", len(streamText), streamText)

	text, title, err := ExtractTextFromPDF([]byte(pdfRaw))
	if err != nil {
		t.Fatalf("Unexpected error extracting text from PDF: %v", err)
	}

	if !strings.Contains(text, "testing PDF text extraction") {
		t.Errorf("Expected text to contain extracted sentence, got: '%s'", text)
	}
	if title == "" {
		t.Errorf("Expected non-empty title")
	}
}

func TestExtractTextFromPDF_FlateDecode(t *testing.T) {
	// Build a zlib-compressed PDF stream
	streamContent := []byte("BT /F1 12 Tf (Compressed PDF stream with FlateDecode containing more than fifteen words for testing decompression logic here now.) Tj ET")
	var b bytes.Buffer
	zw := zlib.NewWriter(&b)
	zw.Write(streamContent)
	zw.Close()
	compressed := b.Bytes()

	pdfRaw := fmt.Sprintf("%%PDF-1.4\n1 0 obj\n<< /Filter /FlateDecode /Length %d >>\nstream\n", len(compressed))
	pdfBytes := append([]byte(pdfRaw), compressed...)
	pdfBytes = append(pdfBytes, []byte("\nendstream\nendobj\ntrailer\n<< /Root 1 0 R >>\n%%EOF")...)

	text, _, err := ExtractTextFromPDF(pdfBytes)
	if err != nil {
		t.Fatalf("Unexpected error extracting text from FlateDecode PDF: %v", err)
	}

	if !strings.Contains(text, "FlateDecode containing more than fifteen words") {
		t.Errorf("Expected decompressed text, got: '%s'", text)
	}
}

func TestExtractTextFromPDF_ShortContentError(t *testing.T) {
	// PDF with only 3 words (< 15 words)
	streamText := "BT /F1 12 Tf (Short PDF document) Tj ET"
	pdfRaw := fmt.Sprintf("%%PDF-1.4\n1 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\ntrailer\n<< /Root 1 0 R >>\n%%%%EOF", len(streamText), streamText)

	_, _, err := ExtractTextFromPDF([]byte(pdfRaw))
	if err == nil {
		t.Fatalf("Expected error for PDF with fewer than 15 words, got nil")
	}
	if !strings.Contains(err.Error(), "fewer than 15 words") {
		t.Errorf("Expected error message to mention word count requirement, got: %v", err)
	}
}

func TestConvertHTMLToMarkdown_PDFRouting(t *testing.T) {
	streamText := "BT /F1 12 Tf (Routing test PDF content with sufficient text to pass fifteen words requirement successfully in extractor.) Tj ET"
	pdfRaw := fmt.Sprintf("%%PDF-1.4\n1 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\ntrailer\n<< /Root 1 0 R >>\n%%%%EOF", len(streamText), streamText)

	md, tokens, title := ConvertHTMLToMarkdown("https://example.com/doc.pdf", []byte(pdfRaw), "clean_rag")
	if tokens <= 0 {
		t.Errorf("Expected positive token count, got %d", tokens)
	}
	if title == "" {
		t.Errorf("Expected valid title")
	}
	if !strings.Contains(md, "Routing test PDF content") {
		t.Errorf("Expected markdown to contain extracted PDF text, got: '%s'", md)
	}
}

func TestParseBTBlockContent_MalformedBounds(t *testing.T) {
	// Test malformed input with trailing backslashes inside literal string / TJ arrays
	malformedBlocks := [][]byte{
		[]byte(`BT (unclosed string with trailing backslash \`),
		[]byte(`BT (unclosed \`),
		[]byte(`BT [ (array unclosed \`),
		[]byte(`BT <1234`),
		[]byte(`BT [ <1234`),
	}

	for i, block := range malformedBlocks {
		t.Run(fmt.Sprintf("Block_%d", i), func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("parseBTBlockContent panicked on malformed input: %v", r)
				}
			}()
			_ = parseBTBlockContent(block)
		})
	}
}

func TestDecompressPDFStream_DecompressionBombLimit(t *testing.T) {
	// Create a large payload (> 10MB)
	largeData := bytes.Repeat([]byte("0123456789abcdef"), 1*1024*1024) // 16MB of raw data
	var b bytes.Buffer
	zw := zlib.NewWriter(&b)
	zw.Write(largeData)
	zw.Close()

	compressed := b.Bytes()
	dict := "/Filter /FlateDecode"

	decomp := decompressPDFStream(dict, compressed)
	if len(decomp) > 10*1024*1024 {
		t.Errorf("Expected decompressed stream size to be capped at 10MB (10485760 bytes), got %d bytes", len(decomp))
	}
	if len(decomp) != 10*1024*1024 {
		t.Errorf("Expected decompressed stream size to equal exactly 10MB limit, got %d bytes", len(decomp))
	}
}

