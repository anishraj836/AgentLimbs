package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
)

// NormalizeURL removes fragments, ensures lowercase scheme/host, strips trailing
// slashes, and validates that the scheme is HTTP or HTTPS.
func NormalizeURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}

	// Force lower case scheme and host
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)

	// Only allow http and https — reject javascript:, mailto:, ftp:, data:, etc.
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("unsupported scheme %q in URL %q", u.Scheme, rawURL)
	}

	// Reject URLs with no host (e.g. bare paths or malformed input)
	if u.Host == "" {
		return "", fmt.Errorf("missing host in URL %q", rawURL)
	}

	// Remove fragment
	u.Fragment = ""

	// Remove trailing slash in path if present (unless path is just "/")
	if len(u.Path) > 1 && strings.HasSuffix(u.Path, "/") {
		u.Path = strings.TrimRight(u.Path, "/")
		if u.RawPath != "" {
			u.RawPath = strings.TrimRight(u.RawPath, "/")
		}
	}

	return u.String(), nil
}

// HashURL computes a SHA256 hash of a normalized URL to use as a primary key
func HashURL(normalizedURL string) string {
	hash := sha256.Sum256([]byte(normalizedURL))
	return hex.EncodeToString(hash[:])
}

// GetDomain extracts the hostname from a raw URL (excluding port)
func GetDomain(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if u.Hostname() == "" {
		return "", fmt.Errorf("missing host in URL %q", rawURL)
	}
	return strings.ToLower(u.Hostname()), nil
}

// TransformGitHubURL converts standard GitHub repository URLs into direct raw Markdown URLs.
func TransformGitHubURL(rawURL string) string {
	if strings.Contains(rawURL, "raw.githubusercontent.com") {
		return rawURL
	}
	if strings.Contains(rawURL, "github.com") {
		parts := strings.Split(rawURL, "github.com/")
		if len(parts) == 2 {
			repoPath := strings.Trim(parts[1], "/")
			segments := strings.Split(repoPath, "/")
			if len(segments) == 2 {
				// If a full repo root URL is provided, prefer master/main raw URL if explicit, else fallback
				return fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/master/README.md", segments[0], segments[1])
			}
		}
	}
	return rawURL
}
