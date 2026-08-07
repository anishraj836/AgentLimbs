package httpclient

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strings"
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

type Client struct {
	client *http.Client
}

func NewClient() *Client {
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
				if isPrivateIP(ip) {
					return nil, fmt.Errorf("blocked request to private/internal IP: %s (%s)", ip.String(), host)
				}
			}

			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
		},
	}

	return &Client{
		client: &http.Client{
			Transport: transport,
			Timeout:   10 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("too many redirects (>10)")
				}
				return nil
			},
		},
	}
}

// FetchResult wraps the HTTP response along with the final URL after redirects.
type FetchResult struct {
	Response *http.Response
	FinalURL string // The URL after all redirects resolved
}

// Fetch executes an HTTP GET request with retries and exponential backoff.
// It accepts a context to support cancellation during graceful shutdown.
func (c *Client) Fetch(ctx context.Context, url string) (*FetchResult, error) {
	var lastErr error
	maxRetries := 3
	backoff := 1 * time.Second

	for i := 0; i < maxRetries; i++ {
		// Check Robots.txt compliance engine before fetching (skip /robots.txt itself to prevent infinite recursion)
		if !strings.HasSuffix(url, "/robots.txt") && !robotstxt.IsAllowed("AntigravityBot", url) {
			return nil, fmt.Errorf("crawling disallowed by robots.txt rules for URL: %s", url)
		}

		// Create a fresh request on every attempt. Re-using a *http.Request
		// after a redirect chain is unsafe because req.URL gets mutated to
		// the redirect target, causing subsequent retries to hit the wrong URL.
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "AntigravityBot/1.0 (+https://example.com/bot)")
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")
		// Do NOT manually set Accept-Encoding; Go handles gzip transparently.

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
			lastErr = fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
			if !sleepWithContext(ctx, backoff) {
				return nil, ctx.Err()
			}
			backoff *= 2
			continue
		}

		finalURL := url
		if resp != nil && resp.Request != nil && resp.Request.URL != nil {
			finalURL = resp.Request.URL.String()
		}

		return &FetchResult{
			Response: resp,
			FinalURL: finalURL,
		}, nil
	}

	return nil, fmt.Errorf("max retries exceeded for %s: %w", url, lastErr)
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
