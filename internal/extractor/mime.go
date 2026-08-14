package extractor

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// IsBinaryContent checks header contentType, http.DetectContentType(bodyBytes), and magic byte signatures
// to identify binary content types (images, video/audio, executables/binaries, archives).
func IsBinaryContent(contentType string, bodyBytes []byte) bool {
	isBinary, _ := detectBinaryMIME(contentType, bodyBytes)
	return isBinary
}

// detectBinaryMIME returns true and the detected MIME type if bodyBytes or contentType represents unsupported binary media.
func detectBinaryMIME(contentType string, bodyBytes []byte) (bool, string) {
	cleanHeader := cleanContentType(contentType)

	// PDF is not classified as unsupported binary content because it has dedicated text extraction.
	if cleanHeader == "application/pdf" || IsPDF(bodyBytes) {
		return false, ""
	}

	// 1. Check magic bytes
	if isMagic, magicMime := detectMagicType(bodyBytes); isMagic {
		if cleanHeader != "" && cleanHeader != "application/octet-stream" && isBinaryMIMEString(cleanHeader) {
			return true, cleanHeader
		}
		return true, magicMime
	}

	// 2. Check clean header content type
	if cleanHeader != "" && isBinaryMIMEString(cleanHeader) {
		return true, cleanHeader
	}

	// 3. Check http.DetectContentType
	if len(bodyBytes) > 0 {
		detected := http.DetectContentType(bodyBytes)
		cleanDet := cleanContentType(detected)
		if isBinaryMIMEString(cleanDet) {
			if cleanHeader != "" && cleanHeader != "application/octet-stream" && isBinaryMIMEString(cleanHeader) {
				return true, cleanHeader
			}
			return true, cleanDet
		}

		// Handle application/octet-stream when body is non-text binary
		if (cleanHeader == "application/octet-stream" || cleanDet == "application/octet-stream") && isNonTextBinary(bodyBytes) {
			if cleanHeader != "" && cleanHeader != "application/octet-stream" {
				return true, cleanHeader
			}
			return true, "application/octet-stream"
		}
	}

	return false, ""
}

func cleanContentType(ct string) string {
	if idx := strings.Index(ct, ";"); idx != -1 {
		ct = ct[:idx]
	}
	return strings.ToLower(strings.TrimSpace(ct))
}

func isBinaryMIMEString(ct string) bool {
	if ct == "" {
		return false
	}
	// SVG is XML text, not binary image
	if ct == "image/svg+xml" {
		return false
	}
	if strings.HasPrefix(ct, "image/") || strings.HasPrefix(ct, "video/") || strings.HasPrefix(ct, "audio/") {
		return true
	}
	binaryApps := map[string]bool{
		"application/zip":              true,
		"application/x-zip-compressed": true,
		"application/x-tar":            true,
		"application/gzip":             true,
		"application/x-gzip":           true,
		"application/x-7z-compressed":  true,
		"application/x-rar-compressed": true,
		"application/vnd.rar":          true,
		"application/x-bzip2":          true,
		"application/wasm":             true,
		"application/x-executable":     true,
		"application/x-elf":            true,
		"application/x-mach-binary":    true,
		"application/x-msdownload":     true,
		"application/x-dosexec":        true,
		"application/java-archive":     true,
		"application/x-java-applet":    true,
		"application/java-vm":          true,
		"application/x-sharedlib":      true,
		"application/x-object":         true,
		"application/x-so":             true,
		"application/x-dll":            true,
	}
	return binaryApps[ct]
}

func isNonTextBinary(bodyBytes []byte) bool {
	limit := len(bodyBytes)
	if limit > 512 {
		limit = 512
	}
	nonPrintable := 0
	for _, b := range bodyBytes[:limit] {
		if b == 0 {
			return true
		}
		if (b < 9 || (b > 13 && b < 32)) && b != 27 {
			nonPrintable++
		}
	}
	return nonPrintable > limit/10
}

func detectMagicType(bodyBytes []byte) (bool, string) {
	if len(bodyBytes) == 0 {
		return false, ""
	}

	// 1. Images
	// PNG
	if bytes.HasPrefix(bodyBytes, []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}) || bytes.HasPrefix(bodyBytes, []byte{0x89, 'P', 'N', 'G'}) {
		return true, "image/png"
	}
	// JPG / JPEG
	if bytes.HasPrefix(bodyBytes, []byte{0xFF, 0xD8, 0xFF}) {
		return true, "image/jpeg"
	}
	// GIF
	if bytes.HasPrefix(bodyBytes, []byte("GIF87a")) || bytes.HasPrefix(bodyBytes, []byte("GIF89a")) {
		return true, "image/gif"
	}
	// WEBP
	if len(bodyBytes) >= 12 && bytes.HasPrefix(bodyBytes, []byte("RIFF")) && string(bodyBytes[8:12]) == "WEBP" {
		return true, "image/webp"
	}
	// ICO
	if len(bodyBytes) >= 4 && (bytes.HasPrefix(bodyBytes, []byte{0x00, 0x00, 0x01, 0x00}) || bytes.HasPrefix(bodyBytes, []byte{0x00, 0x00, 0x02, 0x00})) {
		return true, "image/x-icon"
	}

	// 2. Video / Audio
	// WAV
	if len(bodyBytes) >= 12 && bytes.HasPrefix(bodyBytes, []byte("RIFF")) && string(bodyBytes[8:12]) == "WAVE" {
		return true, "audio/wav"
	}
	// MP4
	if len(bodyBytes) >= 8 && string(bodyBytes[4:8]) == "ftyp" {
		return true, "video/mp4"
	}
	// MP3
	if bytes.HasPrefix(bodyBytes, []byte("ID3")) || (len(bodyBytes) >= 2 && bodyBytes[0] == 0xFF && (bodyBytes[1]&0xE0) == 0xE0) {
		return true, "audio/mpeg"
	}
	// WEBM
	if bytes.HasPrefix(bodyBytes, []byte{0x1A, 0x45, 0xDF, 0xA3}) {
		return true, "video/webm"
	}

	// 3. Executables / Binaries
	// ELF
	if bytes.HasPrefix(bodyBytes, []byte{0x7F, 'E', 'L', 'F'}) {
		return true, "application/x-elf"
	}
	// Mach-O
	if bytes.HasPrefix(bodyBytes, []byte{0xFE, 0xED, 0xFA, 0xCE}) ||
		bytes.HasPrefix(bodyBytes, []byte{0xFE, 0xED, 0xFA, 0xCF}) ||
		bytes.HasPrefix(bodyBytes, []byte{0xCE, 0xFA, 0xED, 0xFE}) ||
		bytes.HasPrefix(bodyBytes, []byte{0xCF, 0xFA, 0xED, 0xFE}) {
		return true, "application/x-mach-binary"
	}
	// PE (MZ)
	if bytes.HasPrefix(bodyBytes, []byte{'M', 'Z'}) {
		return true, "application/x-msdownload"
	}
	// WASM
	if bytes.HasPrefix(bodyBytes, []byte{0x00, 'a', 's', 'm'}) {
		return true, "application/wasm"
	}
	// CLASS (Java Class file)
	if bytes.HasPrefix(bodyBytes, []byte{0xCA, 0xFE, 0xBA, 0xBE}) {
		return true, "application/java-vm"
	}

	// 4. Archives
	// ZIP
	if bytes.HasPrefix(bodyBytes, []byte{'P', 'K', 0x03, 0x04}) ||
		bytes.HasPrefix(bodyBytes, []byte{'P', 'K', 0x05, 0x06}) ||
		bytes.HasPrefix(bodyBytes, []byte{'P', 'K', 0x07, 0x08}) {
		return true, "application/zip"
	}
	// GZ (gzip)
	if bytes.HasPrefix(bodyBytes, []byte{0x1F, 0x8B}) {
		return true, "application/gzip"
	}
	// 7Z (7-zip)
	if bytes.HasPrefix(bodyBytes, []byte{'7', 'z', 0xBC, 0xAF, 0x27, 0x1C}) {
		return true, "application/x-7z-compressed"
	}
	// RAR
	if bytes.HasPrefix(bodyBytes, []byte{'R', 'a', 'r', '!', 0x1A, 0x07}) {
		return true, "application/x-rar-compressed"
	}
	// BZ2 (bzip2)
	if bytes.HasPrefix(bodyBytes, []byte{'B', 'Z', 'h'}) {
		return true, "application/x-bzip2"
	}
	// TAR
	if len(bodyBytes) >= 262 && string(bodyBytes[257:262]) == "ustar" {
		return true, "application/x-tar"
	}

	return false, ""
}

// ExtractDocumentText routes content according to MIME type and magic bytes to format document text as Markdown.
func ExtractDocumentText(sourceURL string, contentType string, bodyBytes []byte, mode string) (markdown string, tokenEstimate int, title string, err error) {
	if isBinary, detectedType := detectBinaryMIME(contentType, bodyBytes); isBinary {
		if detectedType == "" {
			detectedType = "application/octet-stream"
		}
		return "", 0, "", fmt.Errorf("Scrape rejected: Target URL returned unsupported binary media type '%s'. Binary files cannot be indexed as RAG text.", detectedType)
	}

	cleanHeader := cleanContentType(contentType)
	trimmed := bytes.TrimSpace(bodyBytes)

	// 1. PDF
	if cleanHeader == "application/pdf" || IsPDF(bodyBytes) {
		text, pdfTitle, pdfErr := ExtractTextFromPDF(bodyBytes)
		if pdfErr != nil {
			return "", 0, "", pdfErr
		}
		if pdfTitle == "" || pdfTitle == "PDF Document" {
			if sourceURL != "" {
				pdfTitle = sourceURL
			} else {
				pdfTitle = "PDF Document"
			}
		}
		md := text
		if !strings.HasPrefix(strings.TrimSpace(md), "# ") {
			md = fmt.Sprintf("# %s\n\n%s", pdfTitle, md)
		}
		return md, CountBPETokens(md), pdfTitle, nil
	}

	// 2. JSON
	if cleanHeader == "application/json" || cleanHeader == "text/json" ||
		strings.HasPrefix(cleanHeader, "application/json") ||
		(len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[')) {
		var v interface{}
		var pretty string
		if jsonErr := json.Unmarshal(bodyBytes, &v); jsonErr == nil {
			if prettyBytes, prettyErr := json.MarshalIndent(v, "", "  "); prettyErr == nil {
				pretty = string(prettyBytes)
			} else {
				pretty = string(bodyBytes)
			}
		} else {
			pretty = string(bodyBytes)
		}

		jsonTitle := ""
		if obj, ok := v.(map[string]interface{}); ok {
			if t, ok := obj["title"].(string); ok && strings.TrimSpace(t) != "" {
				jsonTitle = strings.TrimSpace(t)
			}
		}
		if jsonTitle == "" {
			if sourceURL != "" {
				jsonTitle = sourceURL
			} else {
				jsonTitle = "JSON Document"
			}
		}

		md := fmt.Sprintf("# %s\n\n```json\n%s\n```", jsonTitle, pretty)
		return md, CountBPETokens(md), jsonTitle, nil
	}

	// 3. XML
	if cleanHeader == "application/xml" || cleanHeader == "text/xml" ||
		strings.HasPrefix(cleanHeader, "application/xml") || strings.HasPrefix(cleanHeader, "text/xml") ||
		bytes.HasPrefix(trimmed, []byte("<?xml")) || bytes.HasPrefix(trimmed, []byte("<xml")) {
		md, xmlTitle := extractCleanXMLText(bodyBytes, sourceURL)
		return md, CountBPETokens(md), xmlTitle, nil
	}

	// 4. Plain Text / Markdown
	if cleanHeader == "text/plain" || cleanHeader == "text/markdown" || cleanHeader == "text/x-markdown" {
		md, textTitle := formatPlainTextMarkdown(bodyBytes, sourceURL)
		return md, CountBPETokens(md), textTitle, nil
	}

	// 5. Default for HTML / DOM
	md, tokens, htmlTitle := ConvertHTMLToMarkdown(sourceURL, bodyBytes, mode)
	return md, tokens, htmlTitle, nil
}

func extractCleanXMLText(xmlBytes []byte, sourceURL string) (string, string) {
	decoder := xml.NewDecoder(bytes.NewReader(xmlBytes))
	var textParts []string
	var inTitleTag bool
	extractedTitle := ""

	for {
		tok, err := decoder.Token()
		if err == io.EOF || err != nil {
			break
		}
		switch elem := tok.(type) {
		case xml.StartElement:
			tagName := strings.ToLower(elem.Name.Local)
			if tagName == "title" || tagName == "h1" {
				inTitleTag = true
			}
		case xml.EndElement:
			tagName := strings.ToLower(elem.Name.Local)
			if tagName == "title" || tagName == "h1" {
				inTitleTag = false
			}
		case xml.CharData:
			txt := strings.TrimSpace(string(elem))
			if txt != "" {
				if inTitleTag && extractedTitle == "" {
					extractedTitle = txt
				}
				textParts = append(textParts, txt)
			}
		}
	}

	xmlTitle := extractedTitle
	if xmlTitle == "" {
		if sourceURL != "" {
			xmlTitle = sourceURL
		} else {
			xmlTitle = "XML Document"
		}
	}

	cleanBody := strings.Join(textParts, "\n\n")
	if cleanBody == "" {
		cleanBody = string(xmlBytes)
	}

	md := cleanBody
	if !strings.HasPrefix(strings.TrimSpace(md), "# ") {
		md = fmt.Sprintf("# %s\n\n%s", xmlTitle, cleanBody)
	}
	return md, xmlTitle
}

func formatPlainTextMarkdown(textBytes []byte, sourceURL string) (string, string) {
	raw := string(textBytes)
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")
	lines := strings.Split(raw, "\n")

	extractedTitle := ""
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "# ") {
			extractedTitle = strings.TrimPrefix(trimmed, "# ")
			extractedTitle = strings.TrimSpace(extractedTitle)
			break
		}
	}

	if extractedTitle == "" {
		for _, l := range lines {
			trimmed := strings.TrimSpace(l)
			if len(trimmed) > 0 && len(trimmed) <= 80 {
				extractedTitle = trimmed
				break
			}
		}
	}

	if extractedTitle == "" {
		if sourceURL != "" {
			extractedTitle = sourceURL
		} else {
			extractedTitle = "Plain Text Document"
		}
	}

	md := strings.TrimSpace(raw)
	if !strings.HasPrefix(md, "# ") {
		md = fmt.Sprintf("# %s\n\n%s", extractedTitle, md)
	}
	return md, extractedTitle
}
