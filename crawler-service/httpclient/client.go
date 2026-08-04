package httpclient

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"time"
)

type Client struct {
	client *http.Client
}

func NewClient() *Client {
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: false},
		// Let Go's transport handle gzip decompression transparently.
		// Do NOT set Accept-Encoding manually; the transport adds it
		// automatically and handles decompression when DisableCompression is false.
		DisableCompression: false,
	}

	return &Client{
		client: &http.Client{
			Transport: transport,
			Timeout:   10 * time.Second,
			// Follow redirects (default behavior) but cap at 10 hops
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
		// Create a fresh request on every attempt. Re-using a *http.Request
		// after a redirect chain is unsafe because req.URL gets mutated to
		// the redirect target, causing subsequent retries to hit the wrong URL.
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "AntigravityBot/1.0 (+https://example.com/bot)")
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
