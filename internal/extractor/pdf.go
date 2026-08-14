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

var (
	reGenericFigure1   = regexp.MustCompile(`(?i)^(making_more_difficult|figure|fig|image|asset|chart|plot|graphic|diagram)[\s_]*[0-9]*`)
	reGenericFigure2   = regexp.MustCompile(`(?i)(figure|fig|image|asset|chart|plot|graphic|diagram)[0-9_]`)
	reArxivHeader      = regexp.MustCompile(`(?i)^arxiv:`)
	reSectionStop      = regexp.MustCompile(`(?i)^(abstract\b|authors?\b|1[\.\s]+introduction\b|introduction\b|table of contents\b)`)
	reBoilerplateText  = regexp.MustCompile(`(?i)^(provided proper attribution|copyright\b|all rights reserved|published in\b|proceedings of\b|doi:|issn:|isbn:|https?://|www\.)`)
	rePageNumber       = regexp.MustCompile(`^(page\s*)?[0-9]+$`)
	reLiteralTitleScan = regexp.MustCompile(`(?m)/Title\s*\(((?:[^\)\\]|\\.)*)\)`)
	reHexTitleScan     = regexp.MustCompile(`(?m)/Title\s*<([0-9a-fA-F\s]+)>`)
	reDCTitleTag       = regexp.MustCompile(`(?is)<dc:title[^>]*>(.*?)</dc:title>`)
	reXMPTitleTag      = regexp.MustCompile(`(?is)<xmp:Title[^>]*>(.*?)</xmp:Title>`)
	reRDFLiTag         = regexp.MustCompile(`(?is)<rdf:li[^>]*>(.*?)</rdf:li>`)
	reXMLTagsStrip     = regexp.MustCompile(`(?s)<[^>]+>`)
)

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
	if reGenericFigure1.MatchString(t) || reGenericFigure2.MatchString(t) {
		return true
	}
	return false
}

var (
	reNonTitleLead       = regexp.MustCompile(`(?i)^(we |there |another |however |in this |this |our |it was |using |these |as |for |from |to |after |then |also |figure |table |section |page |by )`)
	reAuthorLine         = regexp.MustCompile(`(?i)(@|\bgoogle\b|\bmicrosoft\b|\bfacebook\b|\bmeta\b|\bamazon\b|\bapple\b|\bopenai\b|\bdeepmind\b|\buniversity\b|\buniv\b|\bdepartment\b|\bdept\b|\binstitute\b|\binst\b|\bcollege\b|\bschool\b|\blaboratory\b|\blab\b|\bcenter\b|\bcentre\b|\bfaculty\b|\bcorporation\b|\binc\b|\bllc\b)`)
	reStandaloneAbstract = regexp.MustCompile(`(?i)^[ \t]*abstract[ \t]*([—\.\:\-].*)?$`)
)

func isTitleCandidate(s string) bool {
	trimmed := strings.TrimSpace(s)
	runes := []rune(trimmed)
	if len(runes) < 8 || len(runes) > 140 {
		return false
	}
	if !unicode.IsUpper(runes[0]) {
		return false
	}
	lastRune := runes[len(runes)-1]
	if lastRune == '.' || lastRune == ',' || lastRune == ';' || lastRune == '?' {
		return false
	}
	if strings.Count(trimmed, ",") > 1 {
		return false
	}
	if strings.Contains(trimmed, " and ") && (strings.Contains(trimmed, ".") || strings.Contains(trimmed, ",")) {
		return false
	}
	if strings.Contains(trimmed, "[") || strings.Contains(trimmed, "]") {
		return false
	}
	if reNonTitleLead.MatchString(trimmed) || reAuthorLine.MatchString(trimmed) || reArxivHeader.MatchString(trimmed) || reBoilerplateText.MatchString(trimmed) || rePageNumber.MatchString(trimmed) || reSectionStop.MatchString(trimmed) {
		return false
	}
	if strings.Contains(trimmed, "http://") || strings.Contains(trimmed, "https://") || strings.Contains(trimmed, "www.") || strings.Contains(trimmed, "/") {
		return false
	}
	if isGenericTitle(trimmed) {
		return false
	}
	return true
}

var reTitleContinuationEnding = regexp.MustCompile(`(?i)(:|—|-|\bfor|\bin|\bof|\band|\bwith|\bon|\bto|\bvia|\bthrough|\busing|\bfrom|\bby|\bas|\btowards|\bbased on)$`)

func extractTitleFromCleanedText(cleanedText string) string {
	lines := strings.Split(cleanedText, "\n")

	// Strategy 1: Scan for standalone "Abstract" section header on Page 1 and look backwards (up to 15 lines)
	for j, l := range lines {
		trimmed := strings.TrimSpace(l)
		if reStandaloneAbstract.MatchString(trimmed) {
			// Ensure this is a real Page 1 abstract, not a citation reference
			if j+1 < len(lines) {
				nextBody := strings.TrimSpace(lines[j+1])
				if strings.Contains(nextBody, "pages ") || strings.Contains(nextBody, "ACL") || strings.Contains(nextBody, "IEEE") || strings.Contains(nextBody, "arXiv:") {
					continue
				}
			}
			startIdx := 0
			if j > 15 {
				startIdx = j - 15
			}
			for i := startIdx; i < j; i++ {
				cand := strings.TrimSpace(lines[i])
				if isTitleCandidate(cand) {
					if reTitleContinuationEnding.MatchString(cand) && i+1 < j {
						nextCand := strings.TrimSpace(lines[i+1])
						if isValidTitleContinuation(nextCand) {
							return cand + " " + nextCand
						}
					}
					return cand
				}
			}
		}
	}

	// Strategy 2: Forward scan for prominent headline followed within 5 lines by author affiliations, emails, or section headers
	for i, l := range lines {
		cand := strings.TrimSpace(l)
		if !isTitleCandidate(cand) {
			continue
		}
		for j := i + 1; j < len(lines) && j <= i+5; j++ {
			nextL := strings.TrimSpace(lines[j])
			if reStandaloneAbstract.MatchString(nextL) || strings.Contains(nextL, "@") || reAuthorLine.MatchString(nextL) || reSectionStop.MatchString(nextL) {
				if reTitleContinuationEnding.MatchString(cand) && i+1 < j {
					nextCand := strings.TrimSpace(lines[i+1])
					if isValidTitleContinuation(nextCand) {
						return cand + " " + nextCand
					}
				}
				return cand
			}
		}
	}

	// Strategy 3: Top-Headline Scoring Fallback scanning early lines (first 25 lines)
	// Handles out-of-order streams or documents without explicit abstract/author headers
	var bestCandidate string
	var bestScore int

	limit := minInt(len(lines), 25)
	for i := 0; i < limit; i++ {
		cand := strings.TrimSpace(lines[i])
		if !isTitleCandidate(cand) {
			continue
		}
		if reTitleContinuationEnding.MatchString(cand) && i+1 < len(lines) {
			nextCand := strings.TrimSpace(lines[i+1])
			if isValidTitleContinuation(nextCand) {
				cand = cand + " " + nextCand
			}
		}
		score := scoreTitleCandidate(cand)
		if score > bestScore {
			bestScore = score
			bestCandidate = cand
		}
	}

	if bestCandidate != "" && bestScore >= 15 {
		return bestCandidate
	}

	return ""
}

func isValidTitleContinuation(s string) bool {
	trimmed := strings.TrimSpace(s)
	runes := []rune(trimmed)
	if len(runes) < 2 || len(runes) > 90 {
		return false
	}
	lastRune := runes[len(runes)-1]
	if lastRune == '.' || lastRune == ';' || lastRune == '?' {
		return false
	}
	if strings.Count(trimmed, ",") > 1 {
		return false
	}
	if strings.Contains(trimmed, "[") || strings.Contains(trimmed, "]") {
		return false
	}
	if strings.Contains(trimmed, "@") || reAuthorLine.MatchString(trimmed) || reArxivHeader.MatchString(trimmed) ||
		reBoilerplateText.MatchString(trimmed) || reSectionStop.MatchString(trimmed) || rePageNumber.MatchString(trimmed) {
		return false
	}
	if strings.Contains(trimmed, "http://") || strings.Contains(trimmed, "https://") || strings.Contains(trimmed, "www.") || strings.Contains(trimmed, "/") {
		return false
	}
	if isGenericTitle(trimmed) {
		return false
	}
	return true
}

func scoreTitleCandidate(s string) int {
	words := strings.Fields(s)
	wordCount := len(words)
	runeCount := len([]rune(s))

	score := 0
	if wordCount >= 3 && wordCount <= 16 {
		score += 30
	} else if wordCount == 2 {
		score += 10
	}

	if runeCount >= 20 && runeCount <= 100 {
		score += 30
	} else if runeCount >= 12 {
		score += 15
	}

	if strings.Contains(s, ":") || strings.Contains(s, "—") || strings.Contains(s, " - ") {
		score += 15
	}

	if wordCount == 2 && runeCount < 18 && !strings.Contains(s, ":") {
		score -= 20
	}

	return score
}

func extractPDFMetadataTitle(pdfBytes []byte) string {
	// 1. Check Info dictionary /Title (...)
	matches := reLiteralTitleScan.FindAllSubmatch(pdfBytes, -1)
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
	hexMatches := reHexTitleScan.FindAllSubmatch(pdfBytes, -1)
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
	if match := reDCTitleTag.FindSubmatch(data); len(match) > 1 {
		t := cleanXMPTitleContent(match[1])
		if t != "" {
			return t
		}
	}

	if match := reXMPTitleTag.FindSubmatch(data); len(match) > 1 {
		t := cleanXMPTitleContent(match[1])
		if t != "" {
			return t
		}
	}
	return ""
}

func cleanXMPTitleContent(raw []byte) string {
	if match := reRDFLiTag.FindSubmatch(raw); len(match) > 1 {
		raw = match[1]
	}
	cleaned := reXMLTagsStrip.ReplaceAllString(string(raw), "")
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

		// Scan backward from absStreamPos (up to 16,384 bytes) tracking nested >> and << balance
		scanLimit := 16384
		startBound := absStreamPos - scanLimit
		if startBound < 0 {
			startBound = 0
		}
		dictStart := -1
		depth := 0
		foundClose := false

		for k := absStreamPos - 1; k >= startBound+1; k-- {
			if pdfBytes[k-1] == '>' && pdfBytes[k] == '>' {
				depth++
				foundClose = true
				k--
			} else if pdfBytes[k-1] == '<' && pdfBytes[k] == '<' {
				if foundClose {
					depth--
					if depth == 0 {
						dictStart = k - 1
						break
					}
				}
				k--
			}
		}

		if dictStart == -1 {
			// Fallback: simple LastIndex if nested parsing didn't find balanced start
			lastOpening := bytes.LastIndex(pdfBytes[startBound:absStreamPos], []byte("<<"))
			if lastOpening != -1 {
				dictStart = startBound + lastOpening
			}
		}

		dictStr := ""
		if dictStart != -1 && dictStart < absStreamPos {
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

var skipStreamTokens = []string{
	"/type/font",
	"/subtype/image",
	"/subtype/type1c",
	"/subtype/cidfonttype0c",
	"/subtype/opentype",
	"/subtype/truetype",
	"/fontfile",
	"/fontdescriptor",
	"/cidset",
	"/tounicode",
	"/type/metadata",
	"/subtype/xml",
	"/iccbased",
	"/alternate",
	"/colorspace",
	"/dctdecode",
	"/jpxdecode",
	"/jbig2decode",
	"/ccittfaxdecode",
	"/type/xref",
	"/type/objstm",
	"/type/3d",
	"/type/sound",
	"/type/movie",
}

func shouldSkipPDFStream(dict string) bool {
	if dict == "" {
		return false
	}
	var sb strings.Builder
	sb.Grow(len(dict))
	for i := 0; i < len(dict); i++ {
		ch := dict[i]
		if ch != ' ' && ch != '\t' && ch != '\r' && ch != '\n' && ch != '\f' && ch != 0 {
			if ch >= 'A' && ch <= 'Z' {
				sb.WriteByte(ch + 32)
			} else {
				sb.WriteByte(ch)
			}
		}
	}
	norm := sb.String()

	for _, token := range skipStreamTokens {
		if strings.Contains(norm, token) {
			return true
		}
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
	if len(data) == 0 || !bytes.Contains(data, []byte("BT")) || !bytes.Contains(data, []byte("ET")) {
		return nil
	}

	var parts []string
	idx := 0
	n := len(data)
	for idx < n {
		btPos := bytes.Index(data[idx:], []byte("BT"))
		if btPos == -1 {
			break
		}
		absBT := idx + btPos

		// Validate BT is a standalone token bounded by delimiters
		if absBT > 0 && !isDelim(data[absBT-1]) {
			idx = absBT + 2
			continue
		}
		if absBT+2 < n && !isDelim(data[absBT+2]) {
			idx = absBT + 2
			continue
		}

		searchStart := absBT + 2
		searchLimit := minInt(n, absBT+2+128*1024)
		foundET := false
		absET := -1

		for searchStart < searchLimit {
			relET := bytes.Index(data[searchStart:searchLimit], []byte("ET"))
			if relET == -1 {
				break
			}
			candidateET := searchStart + relET
			// Check ET token boundaries
			if (candidateET == 0 || isDelim(data[candidateET-1])) &&
				(candidateET+2 == n || isDelim(data[candidateET+2])) {
				absET = candidateET
				foundET = true
				break
			}
			searchStart = candidateET + 2
		}

		if !foundET || absET <= absBT+2 {
			idx = absBT + 2
			continue
		}

		// Cap max text block size to 128KB to prevent binary false matches
		if absET-(absBT+2) <= 128*1024 {
			blockData := data[absBT+2 : absET]
			extractedStr := parseBTBlockContent(blockData)
			if len(strings.TrimSpace(extractedStr)) > 0 {
				parts = append(parts, extractedStr)
			}
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
					switch val {
					case 0x96: // en-dash
						runes = append(runes, '–')
					case 0x97: // em-dash
						runes = append(runes, '—')
					case 0x95: // bullet
						runes = append(runes, '•')
					case 0x91, 0x92: // quotes
						runes = append(runes, '\'')
					case 0x93, 0x94: // double quotes
						runes = append(runes, '"')
					default:
						runes = append(runes, rune(val))
					}
				} else {
					runes = append(runes, rawRunes[i])
				}
			}
		} else {
			switch rawRunes[i] {
			case 0x96:
				runes = append(runes, '–')
			case 0x97:
				runes = append(runes, '—')
			case 0x95:
				runes = append(runes, '•')
			case 0x91, 0x92:
				runes = append(runes, '\'')
			case 0x93, 0x94:
				runes = append(runes, '"')
			default:
				runes = append(runes, rawRunes[i])
			}
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

var (
	reEnDashDigits           = regexp.MustCompile(`(\d)[\x00-\x1f\x7f-\x9f](\d)`)
	reIsolatedBulletSymbol   = regexp.MustCompile(`(?m)^[ \t]*[\x01-\x08\x0e\x0f\x10-\x1f\x7f\x80-\x9f•◦▪][ \t]*$`)
	reLeadingBullet          = regexp.MustCompile(`(?m)^[ \t]*[\x01-\x08\x0e\x0f\x10-\x1f\x7f\x80-\x9f•◦▪][ \t]+`)
	reHyphenUnicode          = regexp.MustCompile(`(?m)(\p{L}+)-\s*\n\s*(\p{L}+)`)
	reHyphenAscii2           = regexp.MustCompile(`(?m)(\w+)-\s*\n\s*(\w+)`)
	reSpacesGlobal           = regexp.MustCompile(`[ \t]+`)

	// Static pre-compiled lexical ligature repair patterns for unambiguous technical vocabulary
	reFixFirmly        = regexp.MustCompile(`(?i)\brmly\b`)
	reFixFirst         = regexp.MustCompile(`(?i)\brst\b`)
	reFixFlowWord      = regexp.MustCompile(`(?i)\b(information|work|data|signal|cash|air|blood|traffic|control|optical)\s+ow\b`)
	reFixFinalWord     = regexp.MustCompile(`(?i)\b(the|a|its|their|in the|resulting in the)\s+nal\b`)
	reFixBeneficial    = regexp.MustCompile(`(?i)\bbeneci(al|ally|aries|ary)\b`)
	reFixBenefit       = regexp.MustCompile(`(?i)\bbenet(s|ed|ing)?\b`)
	reFixEfficiency    = regexp.MustCompile(`(?i)\b([eE])(?:f|ff)?cien(t|tly|cy|cies)\b`)
	reFixSignificant   = regexp.MustCompile(`(?i)\bsignican(t|tly|ce)\b`)
	reFixDifficult     = regexp.MustCompile(`(?i)\bdifcul(t|ty|ties|tly)\b`)
	reFixSpecific      = regexp.MustCompile(`(?i)\bspeci(c|cally|cation|cations|ed|es|y)\b`)
	reFixScientific    = regexp.MustCompile(`(?i)\bscientic\b`)
	reFixArtificial    = regexp.MustCompile(`(?i)\barticial\b`)
	reFixSuperficial   = regexp.MustCompile(`(?i)\bsupercial\b`)
	reFixSufficient    = regexp.MustCompile(`(?i)\bsufcien(t|tly|cy)\b`)
	reFixInsuffic      = regexp.MustCompile(`(?i)\binsufcien(t|tly|cy)\b`)
	reFixCoefficient   = regexp.MustCompile(`(?i)\bcoecien(t|ts)\b`)
	reFixConflict      = regexp.MustCompile(`(?i)\bconic(t|ts|ted|ting)\b`)
	reFixIdentify      = regexp.MustCompile(`(?i)\bidenti(ed|cation|cations|able|er|ers)\b`)
	reFixModify        = regexp.MustCompile(`(?i)\bmodi(ed|cation|cations|er|ers|able)\b`)
	reFixClassify      = regexp.MustCompile(`(?i)\bclassi(ed|cation|cations|er|ers)\b`)
	reFixVerify        = regexp.MustCompile(`(?i)\bveri(ed|cation|cations|able|er)\b`)
	reFixJustify       = regexp.MustCompile(`(?i)\bjusti(ed|cation|cations|able)\b`)
	reFixCertify       = regexp.MustCompile(`(?i)\bcerti(ed|cation|cations)\b`)
	reFixDefine        = regexp.MustCompile(`(?i)\b([dD])en(e|ed|es|ing|ition|itions)\b`)
	reFixInfinite      = regexp.MustCompile(`(?i)\binnite(ly)?\b`)
	reFixInfinity      = regexp.MustCompile(`(?i)\binnity\b`)
	reFixProfile       = regexp.MustCompile(`(?i)\bprole(s)?\b`)
	reFixConfirm       = regexp.MustCompile(`(?i)\bconrm(s|ed|ing|ation)?\b`)
	reFixFigure        = regexp.MustCompile(`(?i)\bgure(s)?\b`)
	reFixFinancial     = regexp.MustCompile(`(?i)\bnancial(ly)?\b`)
	reFixQualified     = regexp.MustCompile(`(?i)\bqualied\b`)
	reFixQuantify      = regexp.MustCompile(`(?i)\bquanti(ed|able|cation)\b`)
	reFixSimplify      = regexp.MustCompile(`(?i)\b([sS])impli(ed|cation|cations|able|er|ers)\b`)
	reFixInflect       = regexp.MustCompile(`(?i)\b([iI])nect(ional|ions|ion|ive|ed|ing)\b`)
	reFixUnified       = regexp.MustCompile(`(?i)\b([uU])ni(ed|es|y)\b`)
	reFixHighly        = regexp.MustCompile(`(?i)\b([hH])ig(ly|est|er)\b`)
	reFixFindWords     = regexp.MustCompile(`(?i)\bnd\s+(words?|similar|all|the|a|in|out|patterns?|meaningful|nearest|representations)\b`)
	reFixFindVerb      = regexp.MustCompile(`(?i)\b(to|can|could|will|would|may|might|should|must|and|or|we|they|you|i)\s+nd\b`)
	reFixFiveTypes     = regexp.MustCompile(`(?i)\bve\s+(types|categories|classes|methods|approaches|models|layers|dimensions|vectors)\b`)
	reFixDifferent     = regexp.MustCompile(`(?i)\bdieren(t|tly|ce|ces|tiate|tiation)\b`)
	reFixEffective     = regexp.MustCompile(`(?i)\beective(ly|ness)?\b`)
	reFixEffect        = regexp.MustCompile(`(?i)\beect(s|ed|ing)?\b`)
	reFixAffect        = regexp.MustCompile(`(?i)\baect(s|ed|ing|ive)?\b`)
	reFixTraffic       = regexp.MustCompile(`(?i)\btrac\b`)
	reFixOften         = regexp.MustCompile(`(?i)\boen\b`)
	reFixSuffer        = regexp.MustCompile(`(?i)\bsuer(s|ed|ing)?\b`)
	reFixOffer         = regexp.MustCompile(`(?i)\boer(s|ed|ing)?\b`)
	reFixTransformer   = regexp.MustCompile(`(?i)\btransormer(s)?\b`)
	reFixFlight        = regexp.MustCompile(`(?i)\bight(s)?\b`)
	reFixFlexible      = regexp.MustCompile(`(?i)\bexible\b`)
	reFixFlexibility   = regexp.MustCompile(`(?i)\bexibility\b`)
	reFixFluctuate     = regexp.MustCompile(`(?i)\buctuat(e|ed|ing|ion|ions)\b`)

	// Static bullet formatting regexes
	reIsolatedBulletLine = regexp.MustCompile(`(?m)^[ \t]*[•\-\*\x1e\x1f\x01-\x08\x10-\x1a][ \t]*\n[ \t]*(\S+)`)
	reInlineBulletLine   = regexp.MustCompile(`(?m)^[ \t]*[•\x1e\x1f\x01-\x08\x10-\x1a][ \t]+(\S+)`)
)

func decomposeLigatures(s string) string {
	// 1. Convert Type 1 / Latin-1 font encoding bytes (0x96 en-dash, 0x97 em-dash, 0x95 bullet) to valid UTF-8
	rFont := strings.NewReplacer(
		"\u0096", "–",
		"\u0097", "—",
		"\u0095", "•",
		"\x96", "–",
		"\x97", "—",
		"\x95", "•",
		"\x91", "'",
		"\x92", "'",
	)
	s = rFont.Replace(s)

	// 2. Convert control bytes between digits (e.g. 770\x1f778) to en-dash (770–778)
	s = reEnDashDigits.ReplaceAllString(s, "${1}–${2}")

	// 3. Convert standalone bullet control bytes to "•" so they are not stripped by unicode.IsPrint
	s = reIsolatedBulletSymbol.ReplaceAllString(s, "•")

	// 4. Convert leading control bytes on lines to markdown list bullets
	s = reLeadingBullet.ReplaceAllString(s, "- ")

	// 3. Unambiguous Unicode presentation ligatures & explicit TeX ligatures
	r := strings.NewReplacer(
		"\uFB00", "ff",
		"\uFB01", "fi",
		"\uFB02", "fl",
		"\uFB03", "ffi",
		"\uFB04", "ffl",
		"\uFB05", "ft",
		"\uFB06", "st",
		// TeX Type 1 / custom differences ligatures
		"\x01", "ff",
		"\x02", "fi",
		"\x03", "fl",
		"\x04", "ffi",
		"\x05", "ffl",
		// TeX OT1 ligatures
		"\x0b", "ff",
		"\x0c", "fi",
		"\x0d", "fl",
		"\x0e", "ffi",
		"\x0f", "ffl",
		// TeX T1 ligatures
		"\x1b", "ff",
		"\x1c", "fi",
		"\x1d", "fl",
		"\x1e", "ffi",
		"\x1f", "ffl",
		// TeX dotless i / j
		"\x19", "i",
		"\x1a", "j",
	)
	s = r.Replace(s)

	return s
}

// repairCommonMissingLigatures restores missing ligatures in words where custom PDF font encodings drop ligature glyphs.
// All patterns are carefully bounded to multi-letter words to NEVER corrupt names like "Nal Kalchbrenner".
func repairCommonMissingLigatures(text string) string {
	text = reFixFirmly.ReplaceAllString(text, "firmly")
	text = reFixFirst.ReplaceAllString(text, "first")
	text = reFixFlowWord.ReplaceAllString(text, "$1 flow")
	text = reFixFinalWord.ReplaceAllString(text, "$1 final")
	text = reFixBeneficial.ReplaceAllString(text, "benefici$1")
	text = reFixBenefit.ReplaceAllString(text, "benefit$1")
	text = reFixEfficiency.ReplaceAllString(text, "${1}fficien${2}")
	text = reFixSignificant.ReplaceAllString(text, "significan$1")
	text = reFixDifficult.ReplaceAllString(text, "difficul$1")
	text = reFixSpecific.ReplaceAllString(text, "specifi$1")
	text = reFixScientific.ReplaceAllString(text, "scientific")
	text = reFixArtificial.ReplaceAllString(text, "artificial")
	text = reFixSuperficial.ReplaceAllString(text, "superficial")
	text = reFixSufficient.ReplaceAllString(text, "sufficien$1")
	text = reFixInsuffic.ReplaceAllString(text, "insufficien$1")
	text = reFixCoefficient.ReplaceAllString(text, "coefficien$1")
	text = reFixConflict.ReplaceAllString(text, "conflic$1")
	text = reFixIdentify.ReplaceAllString(text, "identifi$1")
	text = reFixModify.ReplaceAllString(text, "modifi$1")
	text = reFixClassify.ReplaceAllString(text, "classifi$1")
	text = reFixVerify.ReplaceAllString(text, "verifi$1")
	text = reFixJustify.ReplaceAllString(text, "justifi$1")
	text = reFixCertify.ReplaceAllString(text, "certifi$1")
	text = reFixDefine.ReplaceAllString(text, "${1}efin${2}")
	text = reFixInfinite.ReplaceAllString(text, "infinite$1")
	text = reFixInfinity.ReplaceAllString(text, "infinity")
	text = reFixProfile.ReplaceAllString(text, "profile$1")
	text = reFixConfirm.ReplaceAllString(text, "confirm$1")
	text = reFixFigure.ReplaceAllString(text, "figure$1")
	text = reFixFinancial.ReplaceAllString(text, "financial$1")
	text = reFixQualified.ReplaceAllString(text, "qualified")
	text = reFixQuantify.ReplaceAllString(text, "quantifi$1")
	text = reFixSimplify.ReplaceAllString(text, "${1}implifi${2}")
	text = reFixInflect.ReplaceAllString(text, "${1}nflect${2}")
	text = reFixUnified.ReplaceAllString(text, "${1}nifi${2}")
	text = reFixHighly.ReplaceAllString(text, "${1}igh${2}")
	text = reFixFindWords.ReplaceAllString(text, "find $1")
	text = reFixFindVerb.ReplaceAllString(text, "$1 find")
	text = reFixFiveTypes.ReplaceAllString(text, "five $1")
	text = reFixDifferent.ReplaceAllString(text, "differen$1")
	text = reFixEffective.ReplaceAllString(text, "effective$1")
	text = reFixEffect.ReplaceAllString(text, "effect$1")
	text = reFixAffect.ReplaceAllString(text, "affect$1")
	text = reFixTraffic.ReplaceAllString(text, "traffic")
	text = reFixOften.ReplaceAllString(text, "often")
	text = reFixSuffer.ReplaceAllString(text, "suffer$1")
	text = reFixOffer.ReplaceAllString(text, "offer$1")
	text = reFixTransformer.ReplaceAllString(text, "transformer$1")
	text = reFixFlight.ReplaceAllString(text, "flight$1")
	text = reFixFlexible.ReplaceAllString(text, "flexible")
	text = reFixFlexibility.ReplaceAllString(text, "flexibility")
	text = reFixFluctuate.ReplaceAllString(text, "fluctuat$1")

	return text
}

func cleanExtractedPDFText(raw string) string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = decomposeLigatures(raw)

	var sb strings.Builder
	for _, r := range raw {
		if unicode.IsPrint(r) || r == '\n' || r == '\t' {
			sb.WriteRune(r)
		}
	}
	text := sb.String()

	// De-hyphenate line breaks: (\w+)-\n(\w+) -> $1$2
	text = reHyphenUnicode.ReplaceAllString(text, "${1}${2}")
	text = reHyphenAscii2.ReplaceAllString(text, "${1}${2}")

	// Repair common dropped ligatures
	text = repairCommonMissingLigatures(text)

	lines := strings.Split(text, "\n")
	var cleanLines []string
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(reSpacesGlobal.ReplaceAllString(lines[i], " "))
		if strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "<<") || strings.HasPrefix(trimmed, ">>") {
			continue
		}
		if trimmed == "•" || trimmed == "-" || trimmed == "*" || trimmed == "◦" || trimmed == "▪" {
			// If this line is an isolated bullet symbol, merge it with the next non-empty line as a markdown bullet
			merged := false
			for k := i + 1; k < len(lines); k++ {
				nextTrimmed := strings.TrimSpace(reSpacesGlobal.ReplaceAllString(lines[k], " "))
				if len(nextTrimmed) > 0 {
					cleanLines = append(cleanLines, "- "+nextTrimmed)
					i = k // Advance past the merged line
					merged = true
					break
				}
			}
			if merged {
				continue
			}
		}
		if strings.HasPrefix(trimmed, "• ") || strings.HasPrefix(trimmed, "* ") || strings.HasPrefix(trimmed, "◦ ") || strings.HasPrefix(trimmed, "▪ ") {
			trimmed = "- " + strings.TrimSpace(trimmed[2:])
		}
		if len(trimmed) > 0 {
			cleanLines = append(cleanLines, trimmed)
		}
	}
	return strings.Join(cleanLines, "\n")
}
