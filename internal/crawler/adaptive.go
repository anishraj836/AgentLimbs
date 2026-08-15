package crawler

import (
	"net/url"
	"path"
	"regexp"
	"strings"
	"sync"
)

var boilerplateTagRegex = regexp.MustCompile(`(?is)<(script|style|svg)[^>]*>.*?</(script|style|svg)>`)

// CleanYieldRatio computes the ratio of useful markdown text to raw HTML excluding scripts and styles.
func CleanYieldRatio(markdownText string, rawHTML string) float64 {
	if len(rawHTML) == 0 {
		return 0.0
	}
	cleanHTML := boilerplateTagRegex.ReplaceAllString(rawHTML, "")
	cleanLen := len(cleanHTML)
	if cleanLen == 0 {
		cleanLen = len(rawHTML)
	}

	ratio := float64(len(markdownText)) / float64(cleanLen)
	if ratio > 1.0 {
		return 1.0
	}
	return ratio
}

// ExtractSubtreePrefix extracts the canonical path prefix up to depth 2 (e.g., "/docs/v1/").
func ExtractSubtreePrefix(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "/"
	}

	cleanPath := path.Clean(parsed.Path)
	if cleanPath == "" || cleanPath == "." || cleanPath == "/" {
		return "/"
	}

	segments := strings.Split(strings.Trim(cleanPath, "/"), "/")
	if len(segments) == 0 {
		return "/"
	}

	if len(segments) >= 2 {
		return "/" + strings.ToLower(segments[0]) + "/" + strings.ToLower(segments[1]) + "/"
	}
	return "/" + strings.ToLower(segments[0]) + "/"
}

// SubtreeTracker dynamically monitors content density across URL subtrees.
type SubtreeTracker struct {
	mu                  sync.RWMutex
	consecutiveLowYield map[string]int
	prunedPrefixes      map[string]int // prefix -> maxAllowedDepth
}

func NewSubtreeTracker() *SubtreeTracker {
	return &SubtreeTracker{
		consecutiveLowYield: make(map[string]int),
		prunedPrefixes:      make(map[string]int),
	}
}

// RecordYield updates subtree density stats with Hub Page exemption and automatic reset.
func (st *SubtreeTracker) RecordYield(rawURL string, yieldRatio float64, linksCount int, depth int) {
	if st == nil {
		return
	}

	// Hub Page Exemption: High outdegree pages (>= 8 links) or root/depth-1 pages are exempt
	if linksCount >= 8 || depth <= 1 {
		return
	}

	prefix := ExtractSubtreePrefix(rawURL)

	st.mu.Lock()
	defer st.mu.Unlock()

	if yieldRatio < 0.02 {
		st.consecutiveLowYield[prefix]++
		if st.consecutiveLowYield[prefix] >= 3 {
			st.prunedPrefixes[prefix] = depth
		}
	} else {
		// Recovery: Any high yield page resets the low-yield counter
		st.consecutiveLowYield[prefix] = 0
	}
}

// IsPruned checks if a URL belongs to a subtree that exceeded the low-yield limit at the specified depth.
func (st *SubtreeTracker) IsPruned(rawURL string, depth int) bool {
	if st == nil {
		return false
	}
	prefix := ExtractSubtreePrefix(rawURL)

	st.mu.RLock()
	defer st.mu.RUnlock()

	maxAllowedDepth, isPruned := st.prunedPrefixes[prefix]
	if isPruned && depth > maxAllowedDepth {
		return true
	}
	return false
}
