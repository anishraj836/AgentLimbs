package extractor

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
)

// IsPDF checks if data starts with or contains %PDF- header.
func IsPDF(data []byte) bool {
	trimmed := bytes.TrimSpace(data)
	if bytes.HasPrefix(trimmed, []byte("%PDF-")) {
		return true
	}
	if len(data) >= 4 && bytes.Contains(data[:minInt(len(data), 1024)], []byte("%PDF-")) {
		return true
	}
	return false
}

// IsPDFContent checks header or content-type string.
func IsPDFContent(data []byte, contentType string) bool {
	if strings.Contains(strings.ToLower(contentType), "application/pdf") {
		return true
	}
	return IsPDF(data)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ExtractTextFromPDF parses PDF byte stream objects and extracts readable text inside BT/ET blocks and Tj/TJ strings.
func ExtractTextFromPDF(pdfBytes []byte) (text string, title string, err error) {
	if !IsPDF(pdfBytes) {
		return "", "", errors.New("invalid PDF content: missing %PDF- header")
	}

	var textParts []string
	streams := extractPDFStreams(pdfBytes)

	for _, s := range streams {
		if shouldSkipPDFStream(s.dict) {
			continue
		}
		decomp := decompressPDFStream(s.dict, s.data)
		extracted := extractTextFromBTBlocks(decomp)
		if len(extracted) > 0 {
			textParts = append(textParts, extracted...)
		}
	}

	// Fallback: if streams didn't yield text, scan raw pdfBytes for BT...ET blocks
	if len(textParts) == 0 {
		rawExtracted := extractTextFromBTBlocks(pdfBytes)
		if len(rawExtracted) > 0 {
			textParts = append(textParts, rawExtracted...)
		}
	}

	combinedText := strings.Join(textParts, "\n\n")
	cleanedText := cleanExtractedPDFText(combinedText)

	words := strings.Fields(cleanedText)
	if len(words) < 15 {
		return "", "", fmt.Errorf("PDF text extraction error: extracted text contains fewer than 15 words (%d words found, scanned image or encrypted PDF)", len(words))
	}

	// Strategy 1: Extract prominent document title from first page text before "Abstract" / "1 Introduction"
	title = extractTitleFromCleanedText(cleanedText)

	// Strategy 2: If text didn't yield a title, inspect /Info metadata and XMP streams
	if title == "" || isGenericTitle(title) {
		metaTitle := extractPDFMetadataTitle(pdfBytes)
		if metaTitle != "" && !isGenericTitle(metaTitle) {
			title = metaTitle
		}
	}

	if title == "" || isGenericTitle(title) {
		title = "PDF Document"
	}

	return cleanedText, title, nil
}

func isGenericTitle(t string) bool {
	t = strings.TrimSpace(t)
	if len(t) == 0 {
		return true
	}
	lower := strings.ToLower(t)
	if lower == "untitled" || lower == "pdf document" || lower == "document" ||
		lower == "default" || lower == "test" || lower == "unknown" ||
		lower == "blank" || lower == "pdf" || lower == "none" || lower == "null" {
		return true
	}
	if strings.HasPrefix(lower, "microsoft word -") ||
		strings.HasPrefix(lower, "word -") ||
		strings.HasPrefix(lower, "latex -") ||
		strings.HasPrefix(lower, "arxiv:") ||
		strings.HasPrefix(lower, "powerpoint -") {
		return true
	}
	if strings.HasSuffix(lower, ".pdf") || strings.HasSuffix(lower, ".docx") ||
		strings.HasSuffix(lower, ".eps") || strings.HasSuffix(lower, ".png") ||
		strings.HasSuffix(lower, ".jpg") || strings.HasSuffix(lower, ".jpeg") ||
		strings.HasSuffix(lower, ".tex") {
		return true
	}
	// Reject figure / graphic asset identifiers containing underscores or file fragments
	if strings.Contains(t, "_") && !strings.Contains(t, " ") {
		return true
	}
	if regexp.MustCompile(`(?i)^(making_more_difficult|figure|fig|image|asset|chart|plot|graphic|diagram)[\s_]*[0-9]*`).MatchString(t) {
		return true
	}
	if regexp.MustCompile(`(?i)(figure|fig|image|asset|chart|plot|graphic|diagram)[0-9_]`).MatchString(t) {
		return true
	}
	return false
}

func extractTitleFromCleanedText(cleanedText string) string {
	lines := strings.Split(cleanedText, "\n")
	reArxiv := regexp.MustCompile(`(?i)^arxiv:`)
	reStop := regexp.MustCompile(`(?i)^(abstract\b|authors?\b|1[\.\s]+introduction\b|introduction\b|table of contents\b)`)
	reBoilerplate := regexp.MustCompile(`(?i)^(provided proper attribution|copyright\b|all rights reserved|published in\b|proceedings of\b|doi:|issn:|isbn:|https?://|www\.)`)
	rePageNum := regexp.MustCompile(`^(page\s*)?[0-9]+$`)

	var candidateLines []string

	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" {
			continue
		}
		if reArxiv.MatchString(trimmed) || strings.Contains(strings.ToLower(trimmed), "arxiv:") {
			continue
		}
		if rePageNum.MatchString(trimmed) {
			continue
		}
		if reBoilerplate.MatchString(trimmed) {
			continue
		}
		if strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "%") {
			continue
		}
		if reStop.MatchString(trimmed) {
			continue
		}

		lower := strings.ToLower(trimmed)
		if strings.Contains(lower, "@") || strings.Contains(lower, "department of") ||
			strings.Contains(lower, "university") || strings.Contains(lower, "laboratory") ||
			strings.Contains(lower, "institute") || strings.Contains(lower, "google research") ||
			strings.Contains(lower, "google brain") {
			continue
		}

		// Reject lines that look like figure captions or figure references
		if strings.HasPrefix(lower, "figure ") || strings.HasPrefix(lower, "fig. ") || strings.HasPrefix(lower, "table ") {
			continue
		}

		if len(trimmed) >= 4 {
			candidateLines = append(candidateLines, trimmed)
		}
	}

	if len(candidateLines) > 0 {
		title := candidateLines[0]
		if len(title) > 120 {
			title = title[:120] + "..."
		}
		return title
	}

	return ""
}

func extractPDFMetadataTitle(pdfBytes []byte) string {
	// 1. Check Info dictionary /Title (...)
	reLiteral := regexp.MustCompile(`(?m)/Title\s*\(((?:[^\)\\]|\\.)*)\)`)
	matches := reLiteral.FindAllSubmatch(pdfBytes, -1)
	for _, match := range matches {
		if len(match) > 1 {
			t := parsePDFLiteralString("(" + string(match[1]) + ")")
			t = strings.TrimSpace(t)
			if len(t) > 3 && !isGenericTitle(t) {
				return t
			}
		}
	}

	// 2. Check Info dictionary /Title <hex>
	reHex := regexp.MustCompile(`(?m)/Title\s*<([0-9a-fA-F\s]+)>`)
	hexMatches := reHex.FindAllSubmatch(pdfBytes, -1)
	for _, match := range hexMatches {
		if len(match) > 1 {
			t := parsePDFHexString("<" + string(match[1]) + ">")
			t = strings.TrimSpace(t)
			if len(t) > 3 && !isGenericTitle(t) {
				return t
			}
		}
	}

	// 3. Check XMP metadata in raw bytes
	if title := extractXMPTitle(pdfBytes); title != "" && !isGenericTitle(title) {
		return title
	}

	return ""
}

func extractXMPTitle(data []byte) string {
	reDCTitle := regexp.MustCompile(`(?is)<dc:title[^>]*>(.*?)</dc:title>`)
	if match := reDCTitle.FindSubmatch(data); len(match) > 1 {
		t := cleanXMPTitleContent(match[1])
		if t != "" {
			return t
		}
	}

	reXMPTitle := regexp.MustCompile(`(?is)<xmp:Title[^>]*>(.*?)</xmp:Title>`)
	if match := reXMPTitle.FindSubmatch(data); len(match) > 1 {
		t := cleanXMPTitleContent(match[1])
		if t != "" {
			return t
		}
	}
	return ""
}

func cleanXMPTitleContent(raw []byte) string {
	reLi := regexp.MustCompile(`(?is)<rdf:li[^>]*>(.*?)</rdf:li>`)
	if match := reLi.FindSubmatch(raw); len(match) > 1 {
		raw = match[1]
	}
	reTags := regexp.MustCompile(`(?s)<[^>]+>`)
	cleaned := reTags.ReplaceAllString(string(raw), "")
	cleaned = html.UnescapeString(cleaned)
	cleaned = strings.TrimSpace(cleaned)
	return cleaned
}

type pdfStream struct {
	dict string
	data []byte
}

func extractPDFStreams(pdfBytes []byte) []pdfStream {
	var streams []pdfStream
	idx := 0
	for idx < len(pdfBytes) {
		streamPos := bytes.Index(pdfBytes[idx:], []byte("stream"))
		if streamPos == -1 {
			break
		}
		absStreamPos := idx + streamPos

		dictStart := bytes.LastIndex(pdfBytes[:absStreamPos], []byte("<<"))
		dictStr := ""
		if dictStart != -1 && absStreamPos-dictStart < 2048 {
			dictStr = string(pdfBytes[dictStart:absStreamPos])
		}

		dataStart := absStreamPos + 6
		if dataStart < len(pdfBytes) && pdfBytes[dataStart] == '\r' {
			dataStart++
		}
		if dataStart < len(pdfBytes) && pdfBytes[dataStart] == '\n' {
			dataStart++
		}

		endPos := bytes.Index(pdfBytes[dataStart:], []byte("endstream"))
		if endPos == -1 {
			break
		}
		absEndPos := dataStart + endPos
		streamData := pdfBytes[dataStart:absEndPos]

		if len(streamData) > 0 && streamData[len(streamData)-1] == '\n' {
			streamData = streamData[:len(streamData)-1]
		}
		if len(streamData) > 0 && streamData[len(streamData)-1] == '\r' {
			streamData = streamData[:len(streamData)-1]
		}

		streams = append(streams, pdfStream{
			dict: dictStr,
			data: streamData,
		})

		idx = absEndPos + 9
	}
	return streams
}

func shouldSkipPDFStream(dict string) bool {
	if dict == "" {
		return false
	}
	lower := strings.ToLower(dict)
	if strings.Contains(lower, "/type /font") || strings.Contains(lower, "/type/font") ||
		strings.Contains(lower, "/type /page") || strings.Contains(lower, "/type/page") ||
		strings.Contains(lower, "/subtype /image") || strings.Contains(lower, "/subtype/image") {
		return true
	}
	return false
}

const maxDecompressedStreamSize = 10 * 1024 * 1024 // 10MB max stream decompression limit

func decompressPDFStream(dict string, raw []byte) []byte {
	if strings.Contains(dict, "/FlateDecode") || bytes.HasPrefix(raw, []byte{0x78, 0x9c}) || bytes.HasPrefix(raw, []byte{0x78, 0x01}) || bytes.HasPrefix(raw, []byte{0x78, 0xda}) {
		zr, err := zlib.NewReader(bytes.NewReader(raw))
		if err == nil {
			limited := io.LimitReader(zr, maxDecompressedStreamSize)
			decomp, err2 := io.ReadAll(limited)
			zr.Close()
			if err2 == nil && len(decomp) > 0 {
				return decomp
			}
		}
		fr := flate.NewReader(bytes.NewReader(raw))
		limited := io.LimitReader(fr, maxDecompressedStreamSize)
		decomp, err3 := io.ReadAll(limited)
		fr.Close()
		if err3 == nil && len(decomp) > 0 {
			return decomp
		}
	}
	return raw
}

func extractTextFromBTBlocks(data []byte) []string {
	var parts []string
	idx := 0
	for idx < len(data) {
		btPos := bytes.Index(data[idx:], []byte("BT"))
		if btPos == -1 {
			break
		}
		absBT := idx + btPos + 2

		etPos := bytes.Index(data[absBT:], []byte("ET"))
		if etPos == -1 {
			break
		}
		absET := absBT + etPos

		blockData := data[absBT:absET]
		extractedStr := parseBTBlockContent(blockData)
		if len(strings.TrimSpace(extractedStr)) > 0 {
			parts = append(parts, extractedStr)
		}

		idx = absET + 2
	}
	return parts
}

func isDelim(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' || ch == '\f' || ch == 0 ||
		ch == '(' || ch == ')' || ch == '<' || ch == '>' || ch == '[' || ch == ']' ||
		ch == '{' || ch == '}' || ch == '/' || ch == '%'
}

func parseBTBlockContent(block []byte) string {
	var lineBuilder strings.Builder
	var blockBuilder strings.Builder

	var operands []string
	var hasTm bool
	var lastTmX, lastTmY float64

	flushLine := func() {
		if lineBuilder.Len() > 0 {
			trimmed := strings.TrimSpace(lineBuilder.String())
			if len(trimmed) > 0 {
				blockBuilder.WriteString(trimmed)
				blockBuilder.WriteString("\n")
			}
			lineBuilder.Reset()
		}
	}

	appendWord := func(str string) {
		if str == "" {
			return
		}
		if lineBuilder.Len() > 0 && !strings.HasSuffix(lineBuilder.String(), " ") && !strings.HasPrefix(str, " ") {
			lineBuilder.WriteString(" ")
		}
		lineBuilder.WriteString(str)
	}

	i := 0
	n := len(block)

	for i < n {
		ch := block[i]

		// Skip whitespace
		if ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' || ch == '\f' || ch == 0 {
			i++
			continue
		}

		// Skip comments
		if ch == '%' {
			for i < n && block[i] != '\r' && block[i] != '\n' {
				i++
			}
			continue
		}

		// Literal string: (...)
		if ch == '(' {
			start := i
			depth := 1
			i++
			for i < n && depth > 0 {
				if block[i] == '\\' {
					if i+1 < n {
						i += 2
					} else {
						i = n
					}
					continue
				}
				if block[i] == '(' {
					depth++
				} else if block[i] == ')' {
					depth--
				}
				i++
			}
			if i > n {
				i = n
			}
			operands = append(operands, string(block[start:i]))
			continue
		}

		// Hex string: <...> (avoid dictionary <<)
		if ch == '<' {
			if i+1 < n && block[i+1] == '<' {
				i += 2
				continue
			}
			start := i
			i++
			for i < n && block[i] != '>' {
				i++
			}
			if i < n && block[i] == '>' {
				i++
			}
			if i > n {
				i = n
			}
			operands = append(operands, string(block[start:i]))
			continue
		}

		// Close dictionary >>
		if ch == '>' {
			if i+1 < n && block[i+1] == '>' {
				i += 2
			} else {
				i++
			}
			continue
		}

		// Array: [...]
		if ch == '[' {
			start := i
			i++
			for i < n && block[i] != ']' {
				if block[i] == '(' {
					depth := 1
					i++
					for i < n && depth > 0 {
						if block[i] == '\\' {
							if i+1 < n {
								i += 2
							} else {
								i = n
							}
							continue
						}
						if block[i] == '(' {
							depth++
						} else if block[i] == ')' {
							depth--
						}
						i++
					}
				} else if block[i] == '<' && (i+1 >= n || block[i+1] != '<') {
					i++
					for i < n && block[i] != '>' {
						i++
					}
					if i < n && block[i] == '>' {
						i++
					}
				} else {
					i++
				}
			}
			if i < n && block[i] == ']' {
				i++
			}
			if i > n {
				i = n
			}
			operands = append(operands, string(block[start:i]))
			continue
		}

		// Name: /Name
		if ch == '/' {
			start := i
			i++
			for i < n && !isDelim(block[i]) {
				i++
			}
			operands = append(operands, string(block[start:i]))
			continue
		}

		// Other token (number, operator, identifier)
		start := i
		for i < n && !isDelim(block[i]) {
			i++
		}
		tok := string(block[start:i])

		switch tok {
		case "T*":
			flushLine()
			operands = operands[:0]
		case "Td":
			if len(operands) >= 2 {
				tx, _ := strconv.ParseFloat(operands[len(operands)-2], 64)
				ty, _ := strconv.ParseFloat(operands[len(operands)-1], 64)
				if math.Abs(ty) > 1e-3 {
					flushLine()
				} else if math.Abs(tx) > 1e-3 {
					if lineBuilder.Len() > 0 && !strings.HasSuffix(lineBuilder.String(), " ") {
						lineBuilder.WriteString(" ")
					}
				}
			}
			operands = operands[:0]
		case "TD":
			if len(operands) >= 2 {
				tx, _ := strconv.ParseFloat(operands[len(operands)-2], 64)
				ty, _ := strconv.ParseFloat(operands[len(operands)-1], 64)
				if math.Abs(ty) > 1e-3 {
					flushLine()
				} else if math.Abs(tx) > 1e-3 {
					if lineBuilder.Len() > 0 && !strings.HasSuffix(lineBuilder.String(), " ") {
						lineBuilder.WriteString(" ")
					}
				}
			}
			operands = operands[:0]
		case "Tm":
			if len(operands) >= 6 {
				e, _ := strconv.ParseFloat(operands[len(operands)-2], 64)
				f, _ := strconv.ParseFloat(operands[len(operands)-1], 64)
				if hasTm {
					if math.Abs(f-lastTmY) > 1e-3 {
						flushLine()
					} else if math.Abs(e-lastTmX) > 1e-3 {
						if lineBuilder.Len() > 0 && !strings.HasSuffix(lineBuilder.String(), " ") {
							lineBuilder.WriteString(" ")
						}
					}
				}
				lastTmX = e
				lastTmY = f
				hasTm = true
			}
			operands = operands[:0]
		case "Tj":
			if len(operands) >= 1 {
				raw := operands[len(operands)-1]
				var parsed string
				if strings.HasPrefix(raw, "(") {
					parsed = parsePDFLiteralString(raw)
				} else if strings.HasPrefix(raw, "<") {
					parsed = parsePDFHexString(raw)
				}
				appendWord(parsed)
			}
			operands = operands[:0]
		case "TJ":
			if len(operands) >= 1 {
				raw := operands[len(operands)-1]
				if strings.HasPrefix(raw, "[") {
					parsed := parseTJArray(raw)
					appendWord(parsed)
				}
			}
			operands = operands[:0]
		case "'":
			flushLine()
			if len(operands) >= 1 {
				raw := operands[len(operands)-1]
				var parsed string
				if strings.HasPrefix(raw, "(") {
					parsed = parsePDFLiteralString(raw)
				} else if strings.HasPrefix(raw, "<") {
					parsed = parsePDFHexString(raw)
				}
				appendWord(parsed)
			}
			operands = operands[:0]
		case "\"":
			flushLine()
			if len(operands) >= 1 {
				raw := operands[len(operands)-1]
				var parsed string
				if strings.HasPrefix(raw, "(") {
					parsed = parsePDFLiteralString(raw)
				} else if strings.HasPrefix(raw, "<") {
					parsed = parsePDFHexString(raw)
				}
				appendWord(parsed)
			}
			operands = operands[:0]
		case "Tf", "TL", "Tc", "Tw", "Ts", "Tr", "Tz":
			operands = operands[:0]
		default:
			operands = append(operands, tok)
		}
	}

	flushLine()
	return blockBuilder.String()
}

func parseTJArray(arrStr string) string {
	var sb strings.Builder
	i := 0
	n := len(arrStr)
	for i < n {
		ch := arrStr[i]
		if ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' || ch == '\f' || ch == '[' || ch == ']' {
			i++
			continue
		}
		if ch == '(' {
			start := i
			depth := 1
			i++
			for i < n && depth > 0 {
				if arrStr[i] == '\\' {
					if i+1 < n {
						i += 2
					} else {
						i = n
					}
					continue
				}
				if arrStr[i] == '(' {
					depth++
				} else if arrStr[i] == ')' {
					depth--
				}
				i++
			}
			if i > n {
				i = n
			}
			rawStr := arrStr[start:i]
			parsed := parsePDFLiteralString(rawStr)
			sb.WriteString(parsed)
		} else if ch == '<' {
			start := i
			i++
			for i < n && arrStr[i] != '>' {
				i++
			}
			if i < n && arrStr[i] == '>' {
				i++
			}
			if i > n {
				i = n
			}
			rawStr := arrStr[start:i]
			parsed := parsePDFHexString(rawStr)
			sb.WriteString(parsed)
		} else if ch == '-' || ch == '+' || (ch >= '0' && ch <= '9') || ch == '.' {
			start := i
			for i < n && (arrStr[i] == '-' || arrStr[i] == '+' || arrStr[i] == '.' || (arrStr[i] >= '0' && arrStr[i] <= '9') || arrStr[i] == 'e' || arrStr[i] == 'E') {
				i++
			}
			numStr := arrStr[start:i]
			val, err := strconv.ParseFloat(numStr, 64)
			if err == nil && val <= -50 {
				if sb.Len() > 0 && !strings.HasSuffix(sb.String(), " ") {
					sb.WriteString(" ")
				}
			}
		} else {
			i++
		}
	}
	return sb.String()
}

func parsePDFLiteralString(s string) string {
	if len(s) < 2 || s[0] != '(' || s[len(s)-1] != ')' {
		return ""
	}
	content := s[1 : len(s)-1]
	var runes []rune
	rawRunes := []rune(content)
	for i := 0; i < len(rawRunes); i++ {
		if rawRunes[i] == '\\' && i+1 < len(rawRunes) {
			i++
			switch rawRunes[i] {
			case 'n':
				runes = append(runes, '\n')
			case 'r':
				runes = append(runes, '\r')
			case 't':
				runes = append(runes, '\t')
			case 'b':
				runes = append(runes, '\b')
			case 'f':
				runes = append(runes, '\f')
			case '(', ')', '\\':
				runes = append(runes, rawRunes[i])
			default:
				if rawRunes[i] >= '0' && rawRunes[i] <= '7' {
					oct := string(rawRunes[i])
					if i+1 < len(rawRunes) && rawRunes[i+1] >= '0' && rawRunes[i+1] <= '7' {
						i++
						oct += string(rawRunes[i])
						if i+1 < len(rawRunes) && rawRunes[i+1] >= '0' && rawRunes[i+1] <= '7' {
							i++
							oct += string(rawRunes[i])
						}
					}
					val, _ := strconv.ParseInt(oct, 8, 32)
					runes = append(runes, rune(val))
				} else {
					runes = append(runes, rawRunes[i])
				}
			}
		} else {
			runes = append(runes, rawRunes[i])
		}
	}

	// Check for UTF-16BE BOM in literal strings: "\xfe\xff..."
	if len(runes) >= 2 && runes[0] == 0xFE && runes[1] == 0xFF {
		b := make([]byte, len(runes))
		for idx, r := range runes {
			b[idx] = byte(r)
		}
		b = b[2:]
		u16 := make([]uint16, len(b)/2)
		for idx := 0; idx < len(u16); idx++ {
			u16[idx] = uint16(b[2*idx])<<8 | uint16(b[2*idx+1])
		}
		return string(utf16.Decode(u16))
	}

	return string(runes)
}

func parsePDFHexString(s string) string {
	if len(s) < 2 || s[0] != '<' || s[len(s)-1] != '>' {
		return ""
	}
	content := strings.ReplaceAll(s[1:len(s)-1], " ", "")
	content = strings.ReplaceAll(content, "\r", "")
	content = strings.ReplaceAll(content, "\n", "")
	content = strings.ReplaceAll(content, "\t", "")
	if len(content)%2 != 0 {
		content += "0"
	}
	b, err := hex.DecodeString(content)
	if err != nil {
		return ""
	}
	// Check for UTF-16BE BOM (0xFE, 0xFF)
	if len(b) >= 2 && b[0] == 0xFE && b[1] == 0xFF {
		b = b[2:]
		u16 := make([]uint16, len(b)/2)
		for i := 0; i < len(u16); i++ {
			u16[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
		}
		return string(utf16.Decode(u16))
	}
	return string(b)
}

func decomposeLigatures(s string) string {
	r := strings.NewReplacer(
		// Unicode presentation form ligatures
		"\uFB00", "ff",
		"\uFB01", "fi",
		"\uFB02", "fl",
		"\uFB03", "ffi",
		"\uFB04", "ffl",
		"\uFB05", "ft",
		"\uFB06", "st",
		// TeX OT1 font encoding ligatures
		"\x0b", "ff",
		"\x0c", "fi",
		"\x0d", "fl",
		"\x0e", "ffi",
		"\x0f", "ffl",
		// LY1 font encoding ligatures
		"\x93", "fi",
		"\x94", "fl",
		"\x95", "ffi",
		"\x96", "ffl",
		"\x97", "ff",
		// TeX dotless i / j
		"\x19", "i",
		"\x1a", "j",
	)
	s = r.Replace(s)

	// Contextual replacements for T1 ligatures
	s = regexp.MustCompile(`(\p{L})\x1b`).ReplaceAllString(s, "${1}ff")
	s = regexp.MustCompile(`\x1b(\p{L})`).ReplaceAllString(s, "ff${1}")
	s = regexp.MustCompile(`(\p{L})\x1c`).ReplaceAllString(s, "${1}fi")
	s = regexp.MustCompile(`\x1c(\p{L})`).ReplaceAllString(s, "fi${1}")
	s = regexp.MustCompile(`(\p{L})\x1d`).ReplaceAllString(s, "${1}fl")
	s = regexp.MustCompile(`\x1d(\p{L})`).ReplaceAllString(s, "fl${1}")
	s = regexp.MustCompile(`(\p{L})\x1e`).ReplaceAllString(s, "${1}ffi")
	s = regexp.MustCompile(`\x1e(\p{L})`).ReplaceAllString(s, "ffi${1}")
	s = regexp.MustCompile(`(\p{L})\x1f`).ReplaceAllString(s, "${1}ffl")
	s = regexp.MustCompile(`\x1f(\p{L})`).ReplaceAllString(s, "ffl${1}")

	return s
}

// repairCommonMissingLigatures restores missing ligatures in words where custom PDF font encodings drop ligature glyphs.
func repairCommonMissingLigatures(text string) string {
	type ligatureFix struct {
		pattern *regexp.Regexp
		replace string
	}

	fixes := []ligatureFix{
		// fi ligatures
		{regexp.MustCompile(`(?i)\brmly\b`), "firmly"},
		{regexp.MustCompile(`(?i)\befcien(t|tly|cy|cies)\b`), "efficien$1"},
		{regexp.MustCompile(`(?i)\bsignican(t|tly|ce)\b`), "significan$1"},
		{regexp.MustCompile(`(?i)\bdifcul(t|ty|ties|tly)\b`), "difficul$1"},
		{regexp.MustCompile(`(?i)\bspeci(c|cally|cation|cations|ed|es|y)\b`), "specifi$1"},
		{regexp.MustCompile(`(?i)\bscientic\b`), "scientific"},
		{regexp.MustCompile(`(?i)\barticial\b`), "artificial"},
		{regexp.MustCompile(`(?i)\bsupercial\b`), "superficial"},
		{regexp.MustCompile(`(?i)\bsufcien(t|tly|cy)\b`), "sufficien$1"},
		{regexp.MustCompile(`(?i)\binsufcien(t|tly|cy)\b`), "insufficien$1"},
		{regexp.MustCompile(`(?i)\bcoecien(t|ts)\b`), "coefficien$1"},
		{regexp.MustCompile(`(?i)\bconic(t|ts|ted|ting)\b`), "conflic$1"},
		{regexp.MustCompile(`(?i)\bidenti(ed|cation|cations|able|er|ers)\b`), "identifi$1"},
		{regexp.MustCompile(`(?i)\bmodi(ed|cation|cations|er|ers|able)\b`), "modifi$1"},
		{regexp.MustCompile(`(?i)\bclassi(ed|cation|cations|er|ers)\b`), "classifi$1"},
		{regexp.MustCompile(`(?i)\bveri(ed|cation|cations|able|er)\b`), "verifi$1"},
		{regexp.MustCompile(`(?i)\bjusti(ed|cation|cations|able)\b`), "justifi$1"},
		{regexp.MustCompile(`(?i)\bnotii(ed|cation|cations)\b`), "notifi$1"},
		{regexp.MustCompile(`(?i)\bcerti(ed|cation|cations)\b`), "certifi$1"},
		{regexp.MustCompile(`(?i)\bdenition(s)?\b`), "definition$1"},
		{regexp.MustCompile(`(?i)\binnite(ly)?\b`), "infinite$1"},
		{regexp.MustCompile(`(?i)\binnity\b`), "infinity"},
		{regexp.MustCompile(`(?i)\bconrm(ation)\b`), "confirm$1"},
		{regexp.MustCompile(`(?i)\bnancial(ly)?\b`), "financial$1"},
		{regexp.MustCompile(`(?i)\bqualied\b`), "qualified"},
		{regexp.MustCompile(`(?i)\bquanti(ed|able|cation)\b`), "quantifi$1"},
		{regexp.MustCompile(`(?i)\bsimpli(ed|cation|cations|fy)\b`), "simplifi$1"},

		// ff / ffi ligatures
		{regexp.MustCompile(`(?i)\bdieren(t|tly|ce|ces|tiate|tiation)\b`), "differen$1"},
		{regexp.MustCompile(`(?i)\beective(ly|ness)?\b`), "effective$1"},
		{regexp.MustCompile(`(?i)\beect(s|ed|ing)?\b`), "effect$1"},
		{regexp.MustCompile(`(?i)\baect(s|ed|ing|ive)?\b`), "affect$1"},
		{regexp.MustCompile(`(?i)\btransormer(s)?\b`), "transformer$1"},

		// fl ligatures
		{regexp.MustCompile(`(?i)\bexible\b`), "flexible"},
		{regexp.MustCompile(`(?i)\bexibility\b`), "flexibility"},
		{regexp.MustCompile(`(?i)\buctuat(e|ed|ing|ion|ions)\b`), "fluctuat$1"},
	}

	for _, fix := range fixes {
		text = fix.pattern.ReplaceAllString(text, fix.replace)
	}

	return text
}

func cleanExtractedPDFText(raw string) string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")

	reEnDash := regexp.MustCompile(`(\d)[\x1e\x1f\x1b\x1d](\d)`)
	raw = reEnDash.ReplaceAllString(raw, "$1–$2")

	reBullet := regexp.MustCompile(`(?m)^\s*[\x1e\x1f\x1b\x01-\x08\x10-\x1a]\s*`)
	raw = reBullet.ReplaceAllString(raw, "- ")

	raw = decomposeLigatures(raw)

	var sb strings.Builder
	for _, r := range raw {
		if unicode.IsPrint(r) || r == '\n' || r == '\t' {
			sb.WriteRune(r)
		}
	}
	text := sb.String()

	// De-hyphenate line breaks: (\w+)-\n(\w+) -> $1$2
	reHyphen := regexp.MustCompile(`(?m)(\p{L}+)-\s*\n\s*(\p{L}+)`)
	text = reHyphen.ReplaceAllString(text, "${1}${2}")

	reHyphenAscii := regexp.MustCompile(`(?m)(\w+)-\s*\n\s*(\w+)`)
	text = reHyphenAscii.ReplaceAllString(text, "${1}${2}")

	// Repair common dropped ligatures
	text = repairCommonMissingLigatures(text)

	reSpaces := regexp.MustCompile(`[ \t]+`)

	lines := strings.Split(text, "\n")
	var cleanLines []string
	for _, l := range lines {
		trimmed := strings.TrimSpace(reSpaces.ReplaceAllString(l, " "))
		if strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "<<") || strings.HasPrefix(trimmed, ">>") {
			continue
		}
		if len(trimmed) > 0 {
			cleanLines = append(cleanLines, trimmed)
		}
	}
	return strings.Join(cleanLines, "\n")
}
