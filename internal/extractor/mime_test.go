package extractor

import (
	"fmt"
	"strings"
	"testing"
)

func TestIsBinaryContent(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        []byte
		expected    bool
	}{
		// Images
		{
			name:        "PNG Magic Bytes",
			contentType: "image/png",
			body:        []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0},
			expected:    true,
		},
		{
			name:        "JPEG Magic Bytes",
			contentType: "image/jpeg",
			body:        []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10},
			expected:    true,
		},
		{
			name:        "GIF Magic Bytes",
			contentType: "image/gif",
			body:        []byte("GIF89a\x01\x00\x01\x00"),
			expected:    true,
		},
		{
			name:        "WEBP Magic Bytes",
			contentType: "image/webp",
			body:        []byte("RIFF\x00\x00\x00\x00WEBP"),
			expected:    true,
		},
		{
			name:        "ICO Magic Bytes",
			contentType: "image/x-icon",
			body:        []byte{0x00, 0x00, 0x01, 0x00},
			expected:    true,
		},
		// Video / Audio
		{
			name:        "MP4 Magic Bytes",
			contentType: "video/mp4",
			body:        []byte{0, 0, 0, 20, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'},
			expected:    true,
		},
		{
			name:        "MP3 Magic Bytes",
			contentType: "audio/mpeg",
			body:        []byte("ID3\x03\x00\x00\x00\x00\x00"),
			expected:    true,
		},
		{
			name:        "WAV Magic Bytes",
			contentType: "audio/wav",
			body:        []byte("RIFF\x00\x00\x00\x00WAVE"),
			expected:    true,
		},
		{
			name:        "WEBM Magic Bytes",
			contentType: "video/webm",
			body:        []byte{0x1A, 0x45, 0xDF, 0xA3},
			expected:    true,
		},
		// Executables / Binaries
		{
			name:        "ELF Magic Bytes",
			contentType: "application/x-elf",
			body:        []byte{0x7F, 'E', 'L', 'F', 0x02, 0x01, 0x01},
			expected:    true,
		},
		{
			name:        "Mach-O Magic Bytes",
			contentType: "application/x-mach-binary",
			body:        []byte{0xFE, 0xED, 0xFA, 0xCE},
			expected:    true,
		},
		{
			name:        "PE Magic Bytes (MZ)",
			contentType: "application/x-msdownload",
			body:        []byte{'M', 'Z', 0x90, 0x00},
			expected:    true,
		},
		{
			name:        "WASM Magic Bytes",
			contentType: "application/wasm",
			body:        []byte{0x00, 'a', 's', 'm', 0x01, 0x00, 0x00, 0x00},
			expected:    true,
		},
		{
			name:        "Java CLASS Magic Bytes",
			contentType: "application/java-vm",
			body:        []byte{0xCA, 0xFE, 0xBA, 0xBE},
			expected:    true,
		},
		{
			name:        "SO Header",
			contentType: "application/x-so",
			body:        []byte("binary content"),
			expected:    true,
		},
		{
			name:        "DLL Header",
			contentType: "application/x-dll",
			body:        []byte("binary content"),
			expected:    true,
		},
		// Archives
		{
			name:        "ZIP Magic Bytes",
			contentType: "application/zip",
			body:        []byte{'P', 'K', 0x03, 0x04, 0, 0, 0, 0},
			expected:    true,
		},
		{
			name:        "TAR Magic Bytes",
			contentType: "application/x-tar",
			body:        append(make([]byte, 257), []byte("ustar")...),
			expected:    true,
		},
		{
			name:        "GZ Magic Bytes",
			contentType: "application/gzip",
			body:        []byte{0x1F, 0x8B, 0x08, 0x00},
			expected:    true,
		},
		{
			name:        "7Z Magic Bytes",
			contentType: "application/x-7z-compressed",
			body:        []byte{'7', 'z', 0xBC, 0xAF, 0x27, 0x1C},
			expected:    true,
		},
		{
			name:        "RAR Magic Bytes",
			contentType: "application/x-rar-compressed",
			body:        []byte{'R', 'a', 'r', '!', 0x1A, 0x07},
			expected:    true,
		},
		{
			name:        "BZ2 Magic Bytes",
			contentType: "application/x-bzip2",
			body:        []byte{'B', 'Z', 'h', '9'},
			expected:    true,
		},
		// Text / Supported Formats (NOT binary)
		{
			name:        "PDF is NOT binary rejected",
			contentType: "application/pdf",
			body:        []byte("%PDF-1.4\n..."),
			expected:    false,
		},
		{
			name:        "HTML is NOT binary",
			contentType: "text/html; charset=utf-8",
			body:        []byte("<html><body>Hello</body></html>"),
			expected:    false,
		},
		{
			name:        "JSON is NOT binary",
			contentType: "application/json",
			body:        []byte(`{"key": "value"}`),
			expected:    false,
		},
		{
			name:        "XML is NOT binary",
			contentType: "application/xml",
			body:        []byte(`<?xml version="1.0"?><root></root>`),
			expected:    false,
		},
		{
			name:        "Plain Text is NOT binary",
			contentType: "text/plain",
			body:        []byte("Just plain text content"),
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsBinaryContent(tt.contentType, tt.body)
			if got != tt.expected {
				t.Errorf("IsBinaryContent(%q) = %v; want %v", tt.name, got, tt.expected)
			}
		})
	}
}

func TestExtractDocumentText_BinaryRejection(t *testing.T) {
	binaryCases := []struct {
		name        string
		contentType string
		body        []byte
		typeStr     string
	}{
		{
			name:        "PNG Image",
			contentType: "image/png",
			body:        []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A},
			typeStr:     "image/png",
		},
		{
			name:        "ZIP Archive",
			contentType: "application/zip",
			body:        []byte{'P', 'K', 0x03, 0x04},
			typeStr:     "application/zip",
		},
		{
			name:        "WASM Binary",
			contentType: "application/wasm",
			body:        []byte{0x00, 'a', 's', 'm', 0x01},
			typeStr:     "application/wasm",
		},
		{
			name:        "MP4 Video",
			contentType: "video/mp4",
			body:        []byte{0, 0, 0, 20, 'f', 't', 'y', 'p'},
			typeStr:     "video/mp4",
		},
	}

	for _, tt := range binaryCases {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, err := ExtractDocumentText("https://example.com/file", tt.contentType, tt.body, "clean_rag")
			if err == nil {
				t.Fatalf("Expected error for binary content %s, got nil", tt.name)
			}
			expectedErr := fmt.Sprintf("Scrape rejected: Target URL returned unsupported binary media type '%s'. Binary files cannot be indexed as RAG text.", tt.typeStr)
			if err.Error() != expectedErr {
				t.Errorf("Expected error '%s', got '%s'", expectedErr, err.Error())
			}
		})
	}
}

func TestExtractDocumentText_PDF(t *testing.T) {
	streamText := "BT /F1 12 Tf (BT block PDF test content with sufficient length to satisfy the fifteen words requirement easily.) Tj ET"
	pdfRaw := fmt.Sprintf("%%PDF-1.4\n1 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\ntrailer\n<< /Root 1 0 R >>\n%%%%EOF", len(streamText), streamText)

	md, tokens, title, err := ExtractDocumentText("https://example.com/test.pdf", "application/pdf", []byte(pdfRaw), "clean_rag")
	if err != nil {
		t.Fatalf("ExtractDocumentText PDF error: %v", err)
	}
	if tokens <= 0 {
		t.Errorf("Expected tokens > 0, got %d", tokens)
	}
	if title == "" {
		t.Errorf("Expected non-empty title")
	}
	if !strings.Contains(md, "BT block PDF test content") {
		t.Errorf("Expected extracted text in markdown, got: %s", md)
	}
}

func TestExtractDocumentText_JSON(t *testing.T) {
	jsonContent := `{"title": "API Documentation", "status": "active", "version": 1.2}`
	md, tokens, title, err := ExtractDocumentText("https://example.com/api.json", "application/json", []byte(jsonContent), "clean_rag")
	if err != nil {
		t.Fatalf("ExtractDocumentText JSON error: %v", err)
	}
	if title != "API Documentation" {
		t.Errorf("Expected title 'API Documentation', got '%s'", title)
	}
	if tokens <= 0 {
		t.Errorf("Expected tokens > 0, got %d", tokens)
	}
	if !strings.Contains(md, "```json") || !strings.Contains(md, `"status": "active"`) {
		t.Errorf("Expected pretty-printed JSON code block, got: %s", md)
	}
}

func TestExtractDocumentText_XML(t *testing.T) {
	xmlContent := `<?xml version="1.0" encoding="UTF-8"?><root><title>XML Spec</title><content>This is clean XML text content for testing.</content></root>`
	md, tokens, title, err := ExtractDocumentText("https://example.com/data.xml", "application/xml", []byte(xmlContent), "clean_rag")
	if err != nil {
		t.Fatalf("ExtractDocumentText XML error: %v", err)
	}
	if title != "XML Spec" {
		t.Errorf("Expected title 'XML Spec', got '%s'", title)
	}
	if tokens <= 0 {
		t.Errorf("Expected tokens > 0, got %d", tokens)
	}
	if !strings.Contains(md, "This is clean XML text content for testing.") {
		t.Errorf("Expected clean text extracted from XML, got: %s", md)
	}
}

func TestExtractDocumentText_PlainText(t *testing.T) {
	plainText := "# Plain Text Note\r\nLine 1 of plain text document.\r\nLine 2 of plain text document."
	md, tokens, title, err := ExtractDocumentText("https://example.com/note.txt", "text/plain", []byte(plainText), "clean_rag")
	if err != nil {
		t.Fatalf("ExtractDocumentText PlainText error: %v", err)
	}
	if title != "Plain Text Note" {
		t.Errorf("Expected title 'Plain Text Note', got '%s'", title)
	}
	if tokens <= 0 {
		t.Errorf("Expected tokens > 0, got %d", tokens)
	}
	if strings.Contains(md, "\r") {
		t.Errorf("Expected line endings to be normalized without CR")
	}
	if !strings.Contains(md, "Line 1 of plain text document.") {
		t.Errorf("Expected text content in markdown, got: %s", md)
	}
}

func TestExtractDocumentText_HTML(t *testing.T) {
	htmlContent := `<!DOCTYPE html><html><head><title>HTML Page</title></head><body><h1>Welcome to HTML</h1><p>Body content paragraph.</p></body></html>`
	md, tokens, title, err := ExtractDocumentText("https://example.com/index.html", "text/html", []byte(htmlContent), "clean_rag")
	if err != nil {
		t.Fatalf("ExtractDocumentText HTML error: %v", err)
	}
	if title != "HTML Page" {
		t.Errorf("Expected title 'HTML Page', got '%s'", title)
	}
	if tokens <= 0 {
		t.Errorf("Expected tokens > 0, got %d", tokens)
	}
	if !strings.Contains(md, "# Welcome to HTML") {
		t.Errorf("Expected converted markdown, got: %s", md)
	}
}
