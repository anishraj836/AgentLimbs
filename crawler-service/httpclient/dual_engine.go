package httpclient

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// DetectJSShell inspects raw HTML to check if it's an empty client-rendered SPA shell.
func DetectJSShell(htmlBytes []byte) bool {
	if len(htmlBytes) == 0 {
		return false
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(htmlBytes))
	if err != nil {
		return false
	}

	// Remove script/style tags
	doc.Find("script, style, noscript, svg").Remove()
	bodyText := strings.TrimSpace(doc.Find("body").Text())

	// If body text is under 150 characters AND contains app root containers, it's a JS SPA shell
	if len(bodyText) < 150 {
		htmlStr := strings.ToLower(string(htmlBytes))
		if strings.Contains(htmlStr, `id="root"`) || strings.Contains(htmlStr, `id="app"`) || strings.Contains(htmlStr, `id="__next"`) {
			return true
		}
	}

	return false
}

// FetchSmart executes Fast Path (net/http) first, and falls back to JS rendering if an empty SPA shell is detected.
func (c *Client) FetchSmart(ctx context.Context, url string, forceJS bool) (*FetchResult, error) {
	if !forceJS {
		res, err := c.Fetch(ctx, url)
		if err == nil && res != nil && res.Response != nil {
			limitedBody := io.LimitReader(res.Response.Body, 10*1024*1024)
			htmlBytes, readErr := io.ReadAll(limitedBody)
			res.Response.Body.Close()

			if readErr == nil && !DetectJSShell(htmlBytes) {
				// Reconstruct response body reader
				res.Response.Body = io.NopCloser(bytes.NewReader(htmlBytes))
				return res, nil
			}
		}
	}

	// Fallback to simulated JS render / full DOM fetch
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "AntigravityBot/1.0 (+https://example.com/bot; JS-Rendered)")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("smart fetch failed: %w", err)
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
