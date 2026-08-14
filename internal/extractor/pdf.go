package extractor

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode"
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

	title = extractPDFTitle(pdfBytes)

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

	if title == "" {
		lines := strings.Split(cleanedText, "\n")
		for _, l := range lines {
			l = strings.TrimSpace(l)
			if len(l) > 3 {
				if len(l) > 80 {
					title = l[:80] + "..."
				} else {
					title = l
				}
				break
			}
		}
	}

	if title == "" {
		title = "PDF Document"
	}

	return cleanedText, title, nil
}

func extractPDFTitle(pdfBytes []byte) string {
	reLiteral := regexp.MustCompile(`/Title\s*\(((?:[^\)\\]|\\.)*)\)`)
	if match := reLiteral.FindSubmatch(pdfBytes); len(match) > 1 {
		t := parsePDFLiteralString("(" + string(match[1]) + ")")
		t = strings.TrimSpace(t)
		if len(t) > 0 {
			return t
		}
	}
	reHex := regexp.MustCompile(`/Title\s*<([0-9a-fA-F\s]+)>`)
	if match := reHex.FindSubmatch(pdfBytes); len(match) > 1 {
		t := parsePDFHexString("<" + string(match[1]) + ">")
		t = strings.TrimSpace(t)
		if len(t) > 0 {
			return t
		}
	}
	return ""
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

func decompressPDFStream(dict string, raw []byte) []byte {
	if strings.Contains(dict, "/FlateDecode") || bytes.HasPrefix(raw, []byte{0x78, 0x9c}) || bytes.HasPrefix(raw, []byte{0x78, 0x01}) || bytes.HasPrefix(raw, []byte{0x78, 0xda}) {
		zr, err := zlib.NewReader(bytes.NewReader(raw))
		if err == nil {
			decomp, err2 := io.ReadAll(zr)
			zr.Close()
			if err2 == nil && len(decomp) > 0 {
				return decomp
			}
		}
		fr := flate.NewReader(bytes.NewReader(raw))
		decomp, err3 := io.ReadAll(fr)
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

func parseBTBlockContent(block []byte) string {
	var lineBuilder strings.Builder
	var blockBuilder strings.Builder

	i := 0
	n := len(block)

	for i < n {
		ch := block[i]
		if ch == '(' {
			start := i
			depth := 1
			i++
			for i < n && depth > 0 {
				if block[i] == '\\' {
					i += 2
					continue
				}
				if block[i] == '(' {
					depth++
				} else if block[i] == ')' {
					depth--
				}
				i++
			}
			rawStr := string(block[start:i])
			op := peekPDFOperator(block[i:])
			if op == "Tj" || op == "'" || op == "\"" || op == "TJ" {
				parsed := parsePDFLiteralString(rawStr)
				if parsed != "" {
					lineBuilder.WriteString(parsed)
				}
			}
		} else if ch == '<' && i+1 < n && block[i+1] != '<' {
			start := i
			i++
			for i < n && block[i] != '>' {
				i++
			}
			if i < n && block[i] == '>' {
				i++
				rawStr := string(block[start:i])
				op := peekPDFOperator(block[i:])
				if op == "Tj" || op == "'" || op == "\"" || op == "TJ" {
					parsed := parsePDFHexString(rawStr)
					if parsed != "" {
						lineBuilder.WriteString(parsed)
					}
				}
			}
		} else if ch == '[' {
			start := i
			i++
			for i < n && block[i] != ']' {
				i++
			}
			if i < n && block[i] == ']' {
				i++
				op := peekPDFOperator(block[i:])
				if op == "TJ" {
					arrayContent := string(block[start:i])
					parsed := parseTJArray(arrayContent)
					if parsed != "" {
						lineBuilder.WriteString(parsed)
					}
				}
			}
		} else if ch == 'T' && i+1 < n && (block[i+1] == '*' || block[i+1] == 'd' || block[i+1] == 'D') {
			if lineBuilder.Len() > 0 {
				blockBuilder.WriteString(strings.TrimSpace(lineBuilder.String()))
				blockBuilder.WriteString("\n")
				lineBuilder.Reset()
			}
			i++
		} else {
			i++
		}
	}

	if lineBuilder.Len() > 0 {
		blockBuilder.WriteString(strings.TrimSpace(lineBuilder.String()))
		lineBuilder.Reset()
	}

	return blockBuilder.String()
}

func peekPDFOperator(rem []byte) string {
	s := strings.TrimSpace(string(rem))
	if strings.HasPrefix(s, "Tj") {
		return "Tj"
	}
	if strings.HasPrefix(s, "TJ") {
		return "TJ"
	}
	if strings.HasPrefix(s, "'") {
		return "'"
	}
	if strings.HasPrefix(s, "\"") {
		return "\""
	}
	return ""
}

func parseTJArray(arrStr string) string {
	var sb strings.Builder
	i := 0
	n := len(arrStr)
	for i < n {
		if arrStr[i] == '(' {
			start := i
			depth := 1
			i++
			for i < n && depth > 0 {
				if arrStr[i] == '\\' {
					i += 2
					continue
				}
				if arrStr[i] == '(' {
					depth++
				} else if arrStr[i] == ')' {
					depth--
				}
				i++
			}
			rawStr := arrStr[start:i]
			parsed := parsePDFLiteralString(rawStr)
			sb.WriteString(parsed)
		} else if arrStr[i] == '<' {
			start := i
			i++
			for i < n && arrStr[i] != '>' {
				i++
			}
			if i < n && arrStr[i] == '>' {
				i++
				rawStr := arrStr[start:i]
				parsed := parsePDFHexString(rawStr)
				sb.WriteString(parsed)
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
	var sb strings.Builder
	runes := []rune(content)
	for i := 0; i < len(runes); i++ {
		if runes[i] == '\\' && i+1 < len(runes) {
			i++
			switch runes[i] {
			case 'n':
				sb.WriteRune('\n')
			case 'r':
				sb.WriteRune('\r')
			case 't':
				sb.WriteRune('\t')
			case 'b':
				sb.WriteRune('\b')
			case 'f':
				sb.WriteRune('\f')
			case '(', ')', '\\':
				sb.WriteRune(runes[i])
			default:
				if runes[i] >= '0' && runes[i] <= '7' {
					oct := string(runes[i])
					if i+1 < len(runes) && runes[i+1] >= '0' && runes[i+1] <= '7' {
						i++
						oct += string(runes[i])
						if i+1 < len(runes) && runes[i+1] >= '0' && runes[i+1] <= '7' {
							i++
							oct += string(runes[i])
						}
					}
					val, _ := strconv.ParseInt(oct, 8, 32)
					sb.WriteRune(rune(val))
				} else {
					sb.WriteRune(runes[i])
				}
			}
		} else {
			sb.WriteRune(runes[i])
		}
	}
	return sb.String()
}

func parsePDFHexString(s string) string {
	if len(s) < 2 || s[0] != '<' || s[len(s)-1] != '>' {
		return ""
	}
	content := strings.ReplaceAll(s[1:len(s)-1], " ", "")
	content = strings.ReplaceAll(content, "\r", "")
	content = strings.ReplaceAll(content, "\n", "")
	if len(content)%2 != 0 {
		content += "0"
	}
	b, err := hex.DecodeString(content)
	if err != nil {
		return ""
	}
	return string(b)
}

func cleanExtractedPDFText(raw string) string {
	var sb strings.Builder
	for _, r := range raw {
		if unicode.IsPrint(r) || r == '\n' || r == '\t' || r == '\r' {
			sb.WriteRune(r)
		} else if r == '\f' {
			sb.WriteRune('\n')
		}
	}
	lines := strings.Split(sb.String(), "\n")
	var cleanLines []string
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "<<") || strings.HasPrefix(trimmed, ">>") {
			continue
		}
		if len(trimmed) > 0 {
			cleanLines = append(cleanLines, trimmed)
		}
	}
	return strings.Join(cleanLines, "\n")
}
