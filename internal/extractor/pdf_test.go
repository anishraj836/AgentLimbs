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

func TestPDFWordSpacing_TJArray(t *testing.T) {
	// Negative kerning numbers (<= -50) represent inter-word spacing adjustments in PDF TJ arrays
	tjInput := `[(Atten) 10 (tion) -250 (Is) -250 (All) -250 (You) -250 (Need)]`
	parsed := parseTJArray(tjInput)
	expected := "Attention Is All You Need"
	if parsed != expected {
		t.Errorf("parseTJArray mismatch. Expected %q, got %q", expected, parsed)
	}

	// Hex strings in TJ arrays with negative kerning
	tjHex := `[<48656c6c6f> -200 <576f726c64>]`
	parsedHex := parseTJArray(tjHex)
	expectedHex := "Hello World"
	if parsedHex != expectedHex {
		t.Errorf("parseTJArray hex mismatch. Expected %q, got %q", expectedHex, parsedHex)
	}

	// Small kerning adjustments (> -50) do not insert space
	tjNoSpace := `[(un) -20 (break) 10 (able)]`
	parsedNoSpace := parseTJArray(tjNoSpace)
	expectedNoSpace := "unbreakable"
	if parsedNoSpace != expectedNoSpace {
		t.Errorf("parseTJArray no-space mismatch. Expected %q, got %q", expectedNoSpace, parsedNoSpace)
	}
}

func TestPDFTextPositioning_Operators(t *testing.T) {
	// Test Td with vertical displacement (ty != 0 -> newline) and horizontal displacement (ty == 0, tx != 0 -> space)
	blockTd := []byte(`
		100 700 Td (First) Tj (Line) Tj
		0 -20 Td (Second) Tj (Line) Tj
		15 0 Td (Continued) Tj
	`)
	extractedTd := parseBTBlockContent(blockTd)
	expectedLines := []string{"First Line", "Second Line Continued"}
	actualLines := strings.Split(strings.TrimSpace(extractedTd), "\n")
	if len(actualLines) != len(expectedLines) {
		t.Fatalf("Expected %d lines, got %d (%q)", len(expectedLines), len(actualLines), extractedTd)
	}
	for i, exp := range expectedLines {
		if actualLines[i] != exp {
			t.Errorf("Line %d mismatch: expected %q, got %q", i, exp, actualLines[i])
		}
	}

	// Test Tm text matrix positioning
	blockTm := []byte(`
		1 0 0 1 50 700 Tm (Title Text) Tj
		1 0 0 1 50 680 Tm (Subtitle Line) Tj
	`)
	extractedTm := parseBTBlockContent(blockTm)
	if !strings.Contains(extractedTm, "Title Text\nSubtitle Line") {
		t.Errorf("Expected vertical Tm displacement to emit newline, got %q", extractedTm)
	}

	// Test T* and ' operators (moving to next line)
	blockTStar := []byte(`
		(First Paragraph) Tj T*
		(Second Paragraph) '
		(Third Paragraph) '
	`)
	extractedTStar := parseBTBlockContent(blockTStar)
	lines := strings.Split(strings.TrimSpace(extractedTStar), "\n")
	if len(lines) != 3 {
		t.Fatalf("Expected 3 lines from T* and ' operators, got %d (%q)", len(lines), extractedTStar)
	}
	if lines[0] != "First Paragraph" || lines[1] != "Second Paragraph" || lines[2] != "Third Paragraph" {
		t.Errorf("Unexpected lines: %v", lines)
	}
}

func TestPDFLigatureDecomposition(t *testing.T) {
	// TeX OT1 ligatures: \x0b (ff), \x0c / \f (fi), \x0d (fl), \x0e (ffi), \x0f (ffl)
	// TeX T1 ligatures: \x1b (ff), \x1c (fi), \x1d (fl), \x1e (ffi), \x1f (ffl)
	// Unicode ligatures: \uFB00 (ff), \uFB01 (fi), \uFB02 (fl), \uFB03 (ffi), \uFB04 (ffl)
	testCases := []struct {
		input    string
		expected string
	}{
		{"\x0crmly", "firmly"},
		{"\x1crmly", "firmly"},
		{"\uFB01rmly", "firmly"},
		{"speci\x0ccally", "specifically"},
		{"speci\x1ccally", "specifically"},
		{"speci\uFB01cally", "specifically"},
		{"di\x0bicult", "difficult"},
		{"di\x1bicult", "difficult"},
		{"e\x0ecient", "efficient"},
		{"e\x1ecient", "efficient"},
		{"\x0dow", "flow"},
		{"\x1dow", "flow"},
		{"\uFB02ow", "flow"},
		{"o\uFB03ce", "office"},
		{"wa\uFB04e", "waffle"},
		{"The \uFB01rm and the \uFB02ag in the o\uFB03ce.", "The firm and the flag in the office."},
	}

	for _, tc := range testCases {
		res := decomposeLigatures(tc.input)
		if res != tc.expected {
			t.Errorf("decomposeLigatures(%q) = %q, expected %q", tc.input, res, tc.expected)
		}
	}

	// Verify repairCommonMissingLigatures handles dropped ligatures from custom PDF encodings
	droppedLigatures := "The model has been rmly established, demonstrating high efciency, signicant gains, and resolving difcult sequence tasks eectively with the Transformer."
	repaired := cleanExtractedPDFText(droppedLigatures)
	if !strings.Contains(repaired, "firmly") {
		t.Errorf("Expected 'firmly' in repaired text, got %q", repaired)
	}
	if !strings.Contains(repaired, "efficiency") {
		t.Errorf("Expected 'efficiency' in repaired text, got %q", repaired)
	}
	if !strings.Contains(repaired, "significant") {
		t.Errorf("Expected 'significant' in repaired text, got %q", repaired)
	}
	if !strings.Contains(repaired, "difficult") {
		t.Errorf("Expected 'difficult' in repaired text, got %q", repaired)
	}
	if !strings.Contains(repaired, "effectively") {
		t.Errorf("Expected 'effectively' in repaired text, got %q", repaired)
	}
	if !strings.Contains(repaired, "Transformer") {
		t.Errorf("Expected 'Transformer' in repaired text, got %q", repaired)
	}

	// Ensure "Nal Kalchbrenner" is not corrupted to "final Kalchbrenner"
	nalTest := cleanExtractedPDFText("Nal Kalchbrenner is finally here.")
	if !strings.Contains(nalTest, "Nal Kalchbrenner") {
		t.Errorf("Expected 'Nal Kalchbrenner' to be preserved, got %q", nalTest)
	}
	if !strings.Contains(nalTest, "finally") {
		t.Errorf("Expected 'finally' to be preserved, got %q", nalTest)
	}

	beneficialTest := cleanExtractedPDFText("it was found to be benecial in the eld.")
	if !strings.Contains(beneficialTest, "beneficial") {
		t.Errorf("Expected 'beneficial' to be restored, got %q", beneficialTest)
	}
}

func TestPDFEnDashAndBullets(t *testing.T) {
	// Test En-Dash between numbers (TeX T1 control bytes and Type 1 0x96 encoding)
	enDashRaw := "Pages 770\x1f778 and 1735\x1e1780 and 832\x96841"
	enDashClean := cleanExtractedPDFText(enDashRaw)
	if !strings.Contains(enDashClean, "770–778") {
		t.Errorf("Expected en-dash between numbers, got %q", enDashClean)
	}
	if !strings.Contains(enDashClean, "1735–1780") {
		t.Errorf("Expected en-dash between numbers, got %q", enDashClean)
	}
	if !strings.Contains(enDashClean, "832–841") {
		t.Errorf("Expected en-dash between numbers, got %q", enDashClean)
	}

	// Test Bullets
	bulletRaw := "•\nFirst item\n•\nSecond item"
	bulletClean := cleanExtractedPDFText(bulletRaw)
	if !strings.Contains(bulletClean, "- First item") {
		t.Errorf("Expected bullet replaced with '- ', got %q", bulletClean)
	}
	if !strings.Contains(bulletClean, "- Second item") {
		t.Errorf("Expected bullet replaced with '- ', got %q", bulletClean)
	}
}

func TestPDFHyphenationAndWhitespaceCleanup(t *testing.T) {
	raw := "The trans-\nformer architecture has demon-\nstrated remarkable per-\nformance in speci\uFB01c   domains."
	cleaned := cleanExtractedPDFText(raw)
	expected := "The transformer architecture has demonstrated remarkable performance in specific domains."
	if cleaned != expected {
		t.Errorf("Hyphenation/whitespace cleanup mismatch.\nExpected: %q\nGot:      %q", expected, cleaned)
	}
}

func TestPDFTitleExtraction_ArXivAndMetadata(t *testing.T) {
	// 1. Info dictionary /Title (...)
	pdfInfoLiteral := []byte("%PDF-1.4\n1 0 obj\n<< /Title (Deep Residual Learning) >>\nendobj\ntrailer\n<< /Info 1 0 R >>\n%%EOF")
	title1 := extractPDFMetadataTitle(pdfInfoLiteral)
	if title1 != "Deep Residual Learning" {
		t.Errorf("Expected title 'Deep Residual Learning', got %q", title1)
	}

	// 2. Info dictionary /Title <hex UTF-16BE>
	// "Attention" in UTF-16BE: FE FF 00 41 00 74 00 74 00 65 00 6E 00 74 00 69 00 6F 00 6E
	pdfInfoHex := []byte("%PDF-1.4\n1 0 obj\n<< /Title <FEFF0041007400740065006E00740069006F006E> >>\nendobj\ntrailer\n<< /Info 1 0 R >>\n%%EOF")
	title2 := extractPDFMetadataTitle(pdfInfoHex)
	if title2 != "Attention" {
		t.Errorf("Expected title 'Attention', got %q", title2)
	}

	// 3. XMP metadata <dc:title>
	pdfXMP := []byte(`%PDF-1.4
1 0 obj
<< /Type /Metadata /Subtype /XML >>
stream
<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about="" xmlns:dc="http://purl.org/dc/elements/1.1/">
   <dc:title>
    <rdf:Alt>
     <rdf:li xml:lang="x-default">BERT: Pre-training of Deep Bidirectional Transformers</rdf:li>
    </rdf:Alt>
   </dc:title>
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>
endstream
endobj
trailer
<< /Root 1 0 R >>
%%EOF`)
	title3 := extractPDFMetadataTitle(pdfXMP)
	if title3 != "BERT: Pre-training of Deep Bidirectional Transformers" {
		t.Errorf("Expected XMP title, got %q", title3)
	}

	// 4. Figure asset title rejection test
	// If a PDF contains an embedded figure with /Title (making_more_difficult5_new), it MUST be rejected in favor of the real paper title
	streamContent := `
BT /F1 12 Tf
(arXiv:1706.03762v7 [cs.CL] 2 Aug 2023) Tj T*
(Provided proper attribution is provided, Google LLC grants permission to copy.) Tj T*
(Attention Is All You Need) Tj T*
(Ashish Vaswani* Noam Shazeer* Niki Parmar* Jakob Uszkoreit*) Tj T*
(Google Brain Google Research) Tj T*
(Abstract) Tj T*
(The dominant sequence transduction models are based on complex recurrent or convolutional neural networks that include an encoder and a decoder. The best performing models also connect the encoder and decoder through an attention mechanism. We propose a new simple network architecture, the Transformer, based solely on attention mechanisms, dispensing with recurrence and convolutions entirely.) Tj
ET`

	pdfArXivWithFigure := fmt.Sprintf("%%PDF-1.4\n1 0 obj\n<< /Type /XObject /Subtype /Form /Title (making_more_difficult5_new) >>\nendobj\n2 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\ntrailer\n<< /Root 2 0 R >>\n%%%%EOF", len(streamContent), streamContent)

	text, docTitle, err := ExtractTextFromPDF([]byte(pdfArXivWithFigure))
	if err != nil {
		t.Fatalf("ExtractTextFromPDF error: %v", err)
	}

	if docTitle != "Attention Is All You Need" {
		t.Errorf("Expected document title 'Attention Is All You Need', got %q (figure asset was not rejected!)", docTitle)
	}

	if !strings.Contains(text, "Attention Is All You Need") {
		t.Errorf("Expected text to contain title, got: %q", text)
	}
	if !strings.Contains(text, "Transformer") {
		t.Errorf("Expected text to contain Transformer, got: %q", text)
	}
}

func TestExtractTitleFromCleanedText_OutOfOrderStreams(t *testing.T) {
	// Simulate out of order streams
	cleanedText := "1 Introduction\nAttention Is All You Need\nAshish Vaswani"
	title := extractTitleFromCleanedText(cleanedText)
	if title != "Attention Is All You Need" {
		t.Errorf("Expected 'Attention Is All You Need', got %q", title)
	}
}

func TestExtractPDFStreams_BalancedReverseDictionaryScanner(t *testing.T) {
	// 1. Nested dictionary with outer /Type /XObject and /Subtype /Image
	pdfWithNestedImage := []byte("%PDF-1.4\n1 0 obj\n<< /Type /XObject /Resources << /Font << /F1 1 0 R >> >> /Subtype /Image /Length 10 >>\nstream\n0123456789\nendstream\nendobj\n2 0 obj\n<< /Length 85 >>\nstream\nBT /F1 12 Tf (Extracted text from valid content stream with enough words to pass requirement.) Tj ET\nendstream\nendobj\ntrailer\n<< /Root 2 0 R >>\n%%EOF")

	streams := extractPDFStreams(pdfWithNestedImage)
	if len(streams) != 2 {
		t.Fatalf("Expected 2 streams, got %d", len(streams))
	}

	// First stream should have outer dictionary captured and should be skipped
	if !shouldSkipPDFStream(streams[0].dict) {
		t.Errorf("Expected stream 0 with image subtype in outer dict to be skipped, dict: %q", streams[0].dict)
	}

	// Second stream should NOT be skipped
	if shouldSkipPDFStream(streams[1].dict) {
		t.Errorf("Expected content stream 1 to NOT be skipped, dict: %q", streams[1].dict)
	}

	// 2. Test comprehensive token filters in shouldSkipPDFStream
	skipTokensTest := []struct {
		dict     string
		expected bool
	}{
		{"<< /Type /Font /Subtype /Type1 >>", true},
		{"<< /Subtype /Image /Filter /DCTDecode >>", true},
		{"<< /Subtype /Type1C >>", true},
		{"<< /Subtype /CIDFontType0C >>", true},
		{"<< /Subtype /OpenType >>", true},
		{"<< /Subtype /TrueType >>", true},
		{"<< /FontFile2 12 0 R >>", true},
		{"<< /FontDescriptor 10 0 R >>", true},
		{"<< /CIDSet 5 0 R >>", true},
		{"<< /ToUnicode 8 0 R >>", true},
		{"<< /Type /Metadata /Subtype /XML >>", true},
		{"<< /Alternate /DeviceRGB /ICCBased 4 0 R >>", true},
		{"<< /ColorSpace /DeviceCMYK >>", true},
		{"<< /Filter /JPXDecode >>", true},
		{"<< /Filter /JBIG2Decode >>", true},
		{"<< /Filter /CCITTFaxDecode >>", true},
		{"<< /Type /XRef >>", true},
		{"<< /Type /ObjStm >>", true},
		{"<< /Type /3D >>", true},
		{"<< /Type /Sound >>", true},
		{"<< /Type /Movie >>", true},
		{"<< /Length 1234 /Filter /FlateDecode >>", false},
		{"<< /Length 500 >>", false},
	}

	for _, tc := range skipTokensTest {
		res := shouldSkipPDFStream(tc.dict)
		if res != tc.expected {
			t.Errorf("shouldSkipPDFStream(%q) = %v, expected %v", tc.dict, res, tc.expected)
		}
	}
}

func TestExtractTextFromBTBlocks_BoundedETScan(t *testing.T) {
	// Block with unclosed BT followed by a valid BT...ET block
	malformed := []byte("BT (unclosed text without ET) " + strings.Repeat("x", 200) + "\nBT (Valid Second Block Text) Tj ET")
	extracted := extractTextFromBTBlocks(malformed)
	if len(extracted) == 0 {
		t.Fatalf("Expected extracted text from valid second block, got nil")
	}
	if !strings.Contains(extracted[0], "Valid Second Block Text") {
		t.Errorf("Expected extracted text to contain 'Valid Second Block Text', got %q", extracted[0])
	}
}

func TestExtractTitle_MultiTierAndContinuationStitching(t *testing.T) {
	// 1. Continuation stitching with 'in' connector (Word2Vec pattern)
	w2vText := "Efficient Estimation of Word Representations in\nVector Space\nTomas Mikolov, Kai Chen, Greg Corrado, Jeffrey Dean\nGoogle Inc., Mountain View, CA\nAbstract\nWe propose two novel model architectures for computing continuous vector representations of words from very large data sets."
	w2vTitle := extractTitleFromCleanedText(w2vText)
	if w2vTitle != "Efficient Estimation of Word Representations in Vector Space" {
		t.Errorf("Expected stitched title 'Efficient Estimation of Word Representations in Vector Space', got %q", w2vTitle)
	}

	// 2. Continuation stitching with 'for' connector (BERT / ResNet pattern)
	bertText := "BERT: Pre-training of Deep Bidirectional Transformers for\nLanguage Understanding\nJacob Devlin Ming-Wei Chang Kenton Lee Kristina Toutanova\nGoogle AI Language\n{jacobdevlin,mingweichang,kentonl,kristout}@google.com\nAbstract\nWe introduce a new language representation model called BERT."
	bertTitle := extractTitleFromCleanedText(bertText)
	if bertTitle != "BERT: Pre-training of Deep Bidirectional Transformers for Language Understanding" {
		t.Errorf("Expected stitched title 'BERT: Pre-training of Deep Bidirectional Transformers for Language Understanding', got %q", bertTitle)
	}

	// 3. Anchor-assisted forward scan without explicit Abstract header
	anchorText := "Scalable Machine Learning Algorithms for Massive Datasets\nJohn Doe, Jane Smith\njohn.doe@university.edu\nDepartment of Computer Science\n1 Introduction\nMassive datasets require scalable algorithms."
	anchorTitle := extractTitleFromCleanedText(anchorText)
	if anchorTitle != "Scalable Machine Learning Algorithms for Massive Datasets" {
		t.Errorf("Expected anchor-assisted title 'Scalable Machine Learning Algorithms for Massive Datasets', got %q", anchorTitle)
	}
}

func TestRepairCommonMissingLigatures_Comprehensive(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		// Efficiency and capitalization
		{"high efciency and ecient algorithms", "high efficiency and efficient algorithms"},
		{"Efciency is important.", "Efficiency is important."},
		// Simplified and avoid simplifify
		{"a simplied approach was proposed", "a simplified approach was proposed"},
		{"Simplied models are fast.", "Simplified models are fast."},
		// Define and capitalization
		{"we dene the objective function", "we define the objective function"},
		{"Dened as follows:", "Defined as follows:"},
		// Inflect
		{"inectional morphology and inective rules", "inflectional morphology and inflective rules"},
		// Unified
		{"a Unied framework for vision", "a Unified framework for vision"},
		{"unied representation", "unified representation"},
		// Highly
		{"a higly scalable network", "a highly scalable network"},
		{"Higly effective method", "Highly effective method"},
		// Find words / Find verb
		{"nd words with similar meaning", "find words with similar meaning"},
		{"nd representations in vector space", "find representations in vector space"},
		{"we can nd meaningful patterns", "we can find meaningful patterns"},
		{"to nd optimal parameters", "to find optimal parameters"},
		// Five types
		{"there are ve types of embeddings", "there are five types of embeddings"},
		{"ve models were evaluated", "five models were evaluated"},
	}

	for _, tc := range testCases {
		res := repairCommonMissingLigatures(tc.input)
		if res != tc.expected {
			t.Errorf("repairCommonMissingLigatures(%q) = %q, expected %q", tc.input, res, tc.expected)
		}
	}
}
