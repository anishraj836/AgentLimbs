package main
import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)
func decomposeLigatures(s string) string {
	r := strings.NewReplacer(
		"\x0e", "ffi",
		"\x0f", "ffl",
		"\x93", "fi",
	)
	s = r.Replace(s)
	s = regexp.MustCompile(`(\p{L})\x1b`).ReplaceAllString(s, "${1}ff")
	s = regexp.MustCompile(`\x1b(\p{L})`).ReplaceAllString(s, "ff${1}")
	return s
}
func cleanExtractedPDFText(raw string) string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = decomposeLigatures(raw)

	reEnDash := regexp.MustCompile(`(\d)[\x1e\x1f\x1b\x1d](\d)`)
	raw = reEnDash.ReplaceAllString(raw, "$1–$2")

	reBullet := regexp.MustCompile(`(?m)^\s*[\x1e\x1f\x1b\x01-\x08\x10-\x1a]\s*`)
	raw = reBullet.ReplaceAllString(raw, "- ")

	var sb strings.Builder
	for _, r := range raw {
		if unicode.IsPrint(r) || r == '\n' || r == '\t' {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}
func main() {
	raw := "Pages 770\x1f778 and 1735\x1e1780"
	fmt.Printf("%q\n", cleanExtractedPDFText(raw))
}
