package httpclient

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/crawler-monorepo/common/robotstxt"
)

// isPrivateIP checks if an IP is loopback, private RFC1918, link-local metadata, or IPv6 private.
func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}
	// Check IPv4 private ranges
	if ip4 := ip.To4(); ip4 != nil {
		switch {
		case ip4[0] == 10: // 10.0.0.0/8
			return true
		case ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31: // 172.16.0.0/12
			return true
		case ip4[0] == 192 && ip4[1] == 168: // 192.168.0.0/16
			return true
		case ip4[0] == 169 && ip4[1] == 254: // 169.254.0.0/16 AWS/GCP metadata
			return true
		}
	} else if len(ip) == net.IPv6len {
		// IPv6 unique local addresses (fc00::/7)
		if ip[0] == 0xfc || ip[0] == 0xfd {
			return true
		}
	}
	return false
}

type HeaderProfile struct {
	UserAgent       string
	SecChUa         string
	SecChUaPlatform string
	SecFetchDest    string
	SecFetchMode    string
	SecFetchSite    string
	Accept          string
	AcceptLanguage  string
}

var Chrome122Profiles = []HeaderProfile{
	{
		UserAgent:       "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36",
		SecChUa:         `"Chromium";v="122", "Not(A:Brand";v="24", "Google Chrome";v="122"`,
		SecChUaPlatform: `"Windows"`,
		SecFetchDest:    "document",
		SecFetchMode:    "navigate",
		SecFetchSite:    "none",
		Accept:          "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7",
		AcceptLanguage:  "en-US,en;q=0.9",
	},
	{
		UserAgent:       "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36",
		SecChUa:         `"Chromium";v="122", "Not(A:Brand";v="24", "Google Chrome";v="122"`,
		SecChUaPlatform: `"macOS"`,
		SecFetchDest:    "document",
		SecFetchMode:    "navigate",
		SecFetchSite:    "same-origin",
		Accept:          "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7",
		AcceptLanguage:  "en-US,en;q=0.9,es;q=0.8",
	},
	{
		UserAgent:       "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36",
		SecChUa:         `"Chromium";v="122", "Not(A:Brand";v="24", "Google Chrome";v="122"`,
		SecChUaPlatform: `"Linux"`,
		SecFetchDest:    "document",
		SecFetchMode:    "navigate",
		SecFetchSite:    "cross-site",
		Accept:          "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7",
		AcceptLanguage:  "en-US,en;q=0.9",
	},
}

var profileIndex uint64

func GetRotatedHeaderProfile() HeaderProfile {
	idx := atomic.AddUint64(&profileIndex, 1)
	return Chrome122Profiles[idx%uint64(len(Chrome122Profiles))]
}

func ApplyAntiBotHeaders(req *http.Request, profile HeaderProfile) {
	req.Header.Set("User-Agent", profile.UserAgent)
	req.Header.Set("Sec-Ch-Ua", profile.SecChUa)
	req.Header.Set("Sec-Ch-Ua-Mobile", "?0")
	req.Header.Set("Sec-Ch-Ua-Platform", profile.SecChUaPlatform)
	req.Header.Set("Sec-Fetch-Dest", profile.SecFetchDest)
	req.Header.Set("Sec-Fetch-Mode", profile.SecFetchMode)
	req.Header.Set("Sec-Fetch-Site", profile.SecFetchSite)
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("Accept", profile.Accept)
	req.Header.Set("Accept-Language", profile.AcceptLanguage)
	req.Header.Set("Upgrade-Insecure-Requests", "1")
}

// IsSPAPlaceholder checks if the HTML content represents an empty client-side rendered SPA root container.
func IsSPAPlaceholder(html string) bool {
	trimmed := strings.TrimSpace(html)
	if trimmed == "" {
		return true
	}
	if DetectJSShell([]byte(html)) {
		return true
	}
	lower := strings.ToLower(trimmed)
	spaPatterns := []string{
		`<div id="root"></div>`,
		`<div id="app"></div>`,
		`<div id="__next"></div>`,
		`<div id="root"/>`,
		`<div id="app"/>`,
		`<div id="__next"/>`,
	}
	for _, p := range spaPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

type Client struct {
	client                 *http.Client
	allowLoopbackForTesting bool
}

func NewClient() *Client {
	c := &Client{}

	dialer := &net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: false},
		DisableCompression:  false,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}

			ips, err := net.LookupIP(host)
			if err != nil {
				return nil, err
			}

			for _, ip := range ips {
				if !c.allowLoopbackForTesting && isPrivateIP(ip) {
					return nil, fmt.Errorf("blocked request to private/internal IP: %s (%s)", ip.String(), host)
				}
			}

			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
		},
	}

	c.client = &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects (>10)")
			}
			return nil
		},
	}

	return c
}

// FetchResult wraps the HTTP response along with the final URL after redirects.
type FetchResult struct {
	Response *http.Response
	FinalURL string // The URL after all redirects resolved
}

// EnsureRobotsCached fetches and caches /robots.txt for a domain if not already cached.
func (c *Client) EnsureRobotsCached(ctx context.Context, targetURL string) {
	reqURL, err := url.Parse(targetURL)
	if err != nil || reqURL.Hostname() == "" {
		return
	}
	domain := reqURL.Hostname()

	// Check if already cached
	if robotstxt.GlobalDomainCache.HasDomainCached(targetURL) {
		return
	}

	// Fetch /robots.txt directly via guarded transport (skipping compliance recursion check)
	robotsURL := reqURL.Scheme + "://" + reqURL.Host + "/robots.txt"
	req, err := http.NewRequestWithContext(ctx, "GET", robotsURL, nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", "AntigravityBot/1.0 (+https://example.com/bot)")

	resp, err := c.client.Do(req)
	if err != nil {
		robotstxt.GlobalDomainCache.FetchAndCache(domain, "")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		robotstxt.GlobalDomainCache.FetchAndCache(domain, "")
		return
	}

	limitedBody := io.LimitReader(resp.Body, 1*1024*1024)
	bodyBytes, err := io.ReadAll(limitedBody)
	if err != nil {
		robotstxt.GlobalDomainCache.FetchAndCache(domain, "")
		return
	}

	robotstxt.GlobalDomainCache.FetchAndCache(domain, string(bodyBytes))
}

// Fetch executes an HTTP GET request with anti-bot header rotation, retries and exponential backoff.
// It accepts a context to support cancellation during graceful shutdown.
func (c *Client) Fetch(ctx context.Context, targetURL string) (*FetchResult, error) {
	var lastErr error
	maxRetries := 3
	backoff := 1 * time.Second

	for i := 0; i < maxRetries; i++ {
		// Ensure Robots.txt is fetched and cached per-domain before checking compliance
		if !strings.HasSuffix(targetURL, "/robots.txt") {
			c.EnsureRobotsCached(ctx, targetURL)
			if !robotstxt.IsAllowed("AntigravityBot", targetURL) {
				return nil, fmt.Errorf("crawling disallowed by robots.txt rules for URL: %s", targetURL)
			}
		}

		req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
		if err != nil {
			return nil, err
		}

		profile := GetRotatedHeaderProfile()
		ApplyAntiBotHeaders(req, profile)

		resp, err := c.client.Do(req)
		if err != nil {
			lastErr = err
			if !sleepWithContext(ctx, backoff) {
				return nil, ctx.Err()
			}
			backoff *= 2
			continue
		}

		// Retry on 5xx errors or 429 (rate limited)
		if resp.StatusCode >= 500 || resp.StatusCode == 429 {
			resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d from %s", resp.StatusCode, targetURL)
			if !sleepWithContext(ctx, backoff) {
				return nil, ctx.Err()
			}
			backoff *= 2
			continue
		}

		finalURL := targetURL
		if resp != nil && resp.Request != nil && resp.Request.URL != nil {
			finalURL = resp.Request.URL.String()
		}

		return &FetchResult{
			Response: resp,
			FinalURL: finalURL,
		}, nil
	}

	return nil, fmt.Errorf("max retries exceeded for %s: %w", targetURL, lastErr)
}

// FetchWithStepping attempts standard fetch first. If HTTP 403, 401, or 429 status is encountered,
// it steps up request headers to high-stealth modern browser headers with jittered backoff.
func (c *Client) FetchWithStepping(ctx context.Context, targetURL string) (*FetchResult, error) {
	res, err := c.Fetch(ctx, targetURL)

	needsStepUp := false
	if err != nil {
		if strings.Contains(err.Error(), "HTTP 429") || strings.Contains(err.Error(), "HTTP 403") || strings.Contains(err.Error(), "HTTP 401") {
			needsStepUp = true
		}
	} else if res != nil && res.Response != nil {
		code := res.Response.StatusCode
		if code == http.StatusForbidden || code == http.StatusUnauthorized || code == http.StatusTooManyRequests {
			needsStepUp = true
			res.Response.Body.Close()
		}
	}

	if !needsStepUp && err == nil && res != nil {
		return res, nil
	}

	// Apply jittered backoff before stepping up request headers
	jitter := time.Duration(50+rand.Intn(100)) * time.Millisecond
	if !sleepWithContext(ctx, jitter) {
		return nil, ctx.Err()
	}

	req, reqErr := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if reqErr != nil {
		return nil, reqErr
	}

	// High-stealth modern browser header profile
	stealthProfile := HeaderProfile{
		UserAgent:       "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36",
		SecChUa:         `"Chromium";v="122", "Not(A:Brand";v="24", "Google Chrome";v="122"`,
		SecChUaPlatform: `"Windows"`,
		SecFetchDest:    "document",
		SecFetchMode:    "navigate",
		SecFetchSite:    "none",
		Accept:          "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7",
		AcceptLanguage:  "en-US,en;q=0.9",
	}
	ApplyAntiBotHeaders(req, stealthProfile)
	req.Header.Set("Cache-Control", "max-age=0")

	resp, doErr := c.client.Do(req)
	if doErr != nil {
		if err != nil {
			return nil, fmt.Errorf("stepping fetch failed after initial error: %w (stepped error: %v)", err, doErr)
		}
		return nil, fmt.Errorf("stepping fetch failed: %w", doErr)
	}

	finalURL := targetURL
	if resp != nil && resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}

	return &FetchResult{
		Response: resp,
		FinalURL: finalURL,
	}, nil
}

// sleepWithContext sleeps for the given duration but returns early if the
// context is cancelled. Returns true if the full sleep completed.
func sleepWithContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

