package crawler

import (
	"math"
	"net/url"
	"path"
	"strings"
	"unicode"
)

var technicalKeywords = map[string]struct{}{
	"docs":            {},
	"doc":             {},
	"guide":           {},
	"guides":          {},
	"api":             {},
	"apis":            {},
	"reference":       {},
	"manual":          {},
	"tutorial":        {},
	"tutorials":       {},
	"architecture":    {},
	"getting-started": {},
	"quickstart":      {},
	"sdk":             {},
	"spec":            {},
	"specification":   {},
	"developer":       {},
	"learn":           {},
	"overview":        {},
}

var negativePatterns = map[string]struct{}{
	"login":      {},
	"signup":     {},
	"signin":     {},
	"cart":       {},
	"checkout":   {},
	"privacy":    {},
	"terms":      {},
	"tag":        {},
	"tags":       {},
	"category":   {},
	"categories": {},
	"author":     {},
	"authors":    {},
	"feed":       {},
	"rss":        {},
	"wp-content": {},
}

var genericStopAnchors = map[string]struct{}{
	"click here": {},
	"read more":  {},
	"next":       {},
	"previous":   {},
	"prev":       {},
	"link":       {},
	"here":       {},
	"more":       {},
}

// RuneShannonEntropy calculates normalized Shannon entropy of runes in an anchor text.
func RuneShannonEntropy(anchor string) float64 {
	trimmed := strings.TrimSpace(anchor)
	if trimmed == "" {
		return 0.05
	}

	lower := strings.ToLower(trimmed)
	if _, isGeneric := genericStopAnchors[lower]; isGeneric {
		return 0.05
	}

	runes := []rune(trimmed)
	n := len(runes)
	if n == 0 {
		return 0.05
	}

	// Calculate rune frequency
	freqs := make(map[rune]int)
	for _, r := range runes {
		if !unicode.IsSpace(r) {
			freqs[r]++
		}
	}

	numRunes := 0
	for _, count := range freqs {
		numRunes += count
	}
	if numRunes == 0 {
		return 0.05
	}

	var entropy float64
	numRunesF := float64(numRunes)
	for _, count := range freqs {
		p := float64(count) / numRunesF
		if p > 0 {
			entropy -= p * math.Log2(p)
		}
	}

	// Max theoretical entropy for unique rune count (clamped to 16)
	maxPossible := math.Log2(math.Min(float64(len(freqs)), 16.0))
	if maxPossible <= 0 {
		maxPossible = 1.0
	}

	normalized := entropy / maxPossible
	lengthScale := math.Min(1.0, float64(n)/6.0)

	score := normalized * lengthScale

	// Check for technical keyword bonus in anchor text
	words := strings.Fields(lower)
	for _, w := range words {
		wClean := strings.Trim(w, ".,!?:;\"'()[]{}<>-")
		if _, isTech := technicalKeywords[wClean]; isTech {
			score += 0.25
			break
		}
	}

	if score > 1.0 {
		return 1.0
	} else if score < 0.05 {
		return 0.05
	}
	return score
}

// PathSemanticScore evaluates the information density of a URL path.
func PathSemanticScore(rawURL string) float64 {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return 0.30
	}

	cleanPath := path.Clean(parsed.Path)
	if cleanPath == "" || cleanPath == "." || cleanPath == "/" {
		return 0.40 // Root URL
	}

	score := 0.30 // Baseline score

	segments := strings.Split(strings.Trim(cleanPath, "/"), "/")
	hasTechKeyword := false

	for _, seg := range segments {
		segLower := strings.ToLower(seg)
		if _, isTech := technicalKeywords[segLower]; isTech {
			hasTechKeyword = true
		}
	}

	if hasTechKeyword {
		score += 0.40
	}

	// Extension bonus
	ext := strings.ToLower(path.Ext(cleanPath))
	if ext == ".md" || ext == ".rst" || ext == ".html" || ext == ".json" {
		score += 0.15
	}

	// Deep segment penalty for non-documentation URLs
	if len(segments) > 5 && !hasTechKeyword {
		score -= 0.05 * float64(len(segments)-5)
	}

	if score > 1.0 {
		return 1.0
	} else if score < 0.05 {
		return 0.05
	}
	return score
}

// HasNegativePattern checks if any path segment matches junk patterns.
func HasNegativePattern(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	for _, seg := range segments {
		segLower := strings.ToLower(seg)
		if _, isNeg := negativePatterns[segLower]; isNeg {
			return true
		}
	}
	return false
}

// ComputePriority calculates a composite information-entropy score P in [0.05, 1.0].
func ComputePriority(rawURL string, anchorText string, depth int) float64 {
	sPath := PathSemanticScore(rawURL)
	sAnchor := RuneShannonEntropy(anchorText)
	sDepth := 1.0 / (1.0 + 0.35*float64(depth))

	// Weights: w1 = 0.45, w2 = 0.35, w3 = 0.20
	priority := 0.45*sPath + 0.35*sAnchor + 0.20*sDepth

	// Apply negative pattern dampener
	if HasNegativePattern(rawURL) {
		priority *= 0.15
	}

	if math.IsNaN(priority) || math.IsInf(priority, 0) {
		return 0.05
	}

	if priority > 1.0 {
		return 1.0
	} else if priority < 0.05 {
		return 0.05
	}
	return math.Round(priority*10000) / 10000
}
