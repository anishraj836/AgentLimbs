package crawler

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/crawler-monorepo/common/middleware"
	"github.com/crawler-monorepo/common/tracing"
	"golang.org/x/sync/singleflight"
)

var mediaExtensions = map[string]bool{
	// Images
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".svg": true, ".webp": true, ".ico": true, ".bmp": true,
	".tiff": true, ".tif": true, ".avif": true, ".heic": true,
	// Video & Audio
	".mp4": true, ".webm": true, ".mkv": true, ".avi": true,
	".mov": true, ".wmv": true, ".flv": true, ".m4v": true,
	".mp3": true, ".wav": true, ".ogg": true, ".m4a": true,
	".flac": true, ".aac": true, ".opus": true, ".wma": true,
	// Fonts
	".ttf": true, ".woff": true, ".woff2": true, ".eot": true, ".otf": true,
}

// IsMediaResourceURL checks if a target URL points to an image, video, audio, or font resource.
// Core web pages, CSS (.css), JavaScript (.js, .mjs), and JSON (.json) return false (not blocked).
func IsMediaResourceURL(targetURL string) bool {
	u, err := url.Parse(targetURL)
	if err != nil {
		return false
	}
	path := u.Path
	if idx := strings.Index(path, ";"); idx >= 0 {
		path = path[:idx]
	}
	path = strings.TrimRight(path, "/")
	ext := filepath.Ext(strings.ToLower(path))
	if ext == "" {
		return false
	}
	return mediaExtensions[ext]
}

// Private IP Checking

func getIPFromAddr(addr net.Addr) net.IP {
	if addr == nil {
		return nil
	}
	switch v := addr.(type) {
	case *net.TCPAddr:
		return v.IP
	case *net.UDPAddr:
		return v.IP
	case *net.IPAddr:
		return v.IP
	default:
		host, _, err := net.SplitHostPort(addr.String())
		if err == nil {
			return net.ParseIP(host)
		}
		return net.ParseIP(addr.String())
	}
}

// IsPrivateIP checks if an IP is loopback, private RFC1918, CGNAT, link-local metadata, or IPv6 private.
func IsPrivateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		switch {
		case ip4[0] == 0: // 0.0.0.0/8
			return true
		case ip4[0] == 10: // 10.0.0.0/8
			return true
		case ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127: // 100.64.0.0/10 CGNAT
			return true
		case ip4[0] == 127: // 127.0.0.0/8 Loopback
			return true
		case ip4[0] == 169 && ip4[1] == 254: // 169.254.0.0/16 Link-local / AWS metadata
			return true
		case ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31: // 172.16.0.0/12
			return true
		case ip4[0] == 192 && ip4[1] == 168: // 192.168.0.0/16
			return true
		case ip4[0] == 192 && ip4[1] == 0 && ip4[2] == 2: // TEST-NET-1
			return true
		case ip4[0] == 198 && ip4[1] == 51 && ip4[2] == 100: // TEST-NET-2
			return true
		case ip4[0] == 203 && ip4[1] == 0 && ip4[2] == 113: // TEST-NET-3
			return true
		case ip4[0] >= 224: // Multicast / Reserved (224.0.0.0/4)
			return true
		}
	} else if len(ip) == net.IPv6len {
		switch {
		case ip[0] == 0xfc || ip[0] == 0xfd: // fc00::/7 ULA
			return true
		case ip[0] == 0x20 && ip[1] == 0x01 && ip[2] == 0x0d && ip[3] == 0xb8: // 2001:db8::/32 Documentation
			return true
		case ip[0] == 0x01 && ip[1] == 0x00 && ip[2] == 0 && ip[3] == 0 && ip[4] == 0 && ip[5] == 0 && ip[6] == 0 && ip[7] == 0: // 100::/64 Discard
			return true
		case ip[0] == 0x20 && ip[1] == 0x02: // 2002::/16 6to4
			return true
		}
	}
	return false
}

// Anti-Bot Header Profiles & Rotation

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

// SPA Detection

func DetectJSShell(htmlContent []byte) bool {
	trimmed := strings.TrimSpace(string(htmlContent))
	if len(trimmed) < 20 {
		return true
	}
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, `<div id="root"></div>`) || strings.Contains(lower, `<div id="app"></div>`) || strings.Contains(lower, `<div id="__next"></div>`) {
		return true
	}
	return false
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

// Robots.txt Domain Cache

type RobotsGroup struct {
	UserAgent  string
	Disallowed []string
	CrawlDelay int
}

type RobotsData struct {
	mu     sync.RWMutex
	groups []RobotsGroup
}

func ParseRobotsTxt(content string) *RobotsData {
	rd := &RobotsData{groups: make([]RobotsGroup, 0)}
	scanner := bufio.NewScanner(strings.NewReader(content))

	var currentGroup *RobotsGroup

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if idx := strings.Index(line, "#"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) < 2 {
			continue
		}

		key := strings.ToLower(strings.TrimSpace(parts[0]))
		val := strings.TrimSpace(parts[1])

		switch key {
		case "user-agent":
			if currentGroup != nil {
				rd.groups = append(rd.groups, *currentGroup)
			}
			currentGroup = &RobotsGroup{
				UserAgent:  strings.ToLower(val),
				Disallowed: make([]string, 0),
			}
		case "disallow":
			if currentGroup != nil && val != "" {
				currentGroup.Disallowed = append(currentGroup.Disallowed, val)
			}
		}
	}

	if currentGroup != nil {
		rd.groups = append(rd.groups, *currentGroup)
	}

	return rd
}

func (rd *RobotsData) IsAllowed(userAgent, targetURL string) bool {
	rd.mu.RLock()
	defer rd.mu.RUnlock()

	reqURL, err := url.Parse(targetURL)
	reqPath := targetURL
	if err == nil {
		reqPath = reqURL.Path
	}
	if reqPath == "" {
		reqPath = "/"
	}

	uaLower := strings.ToLower(userAgent)

	for _, group := range rd.groups {
		if group.UserAgent == "*" || strings.Contains(uaLower, group.UserAgent) {
			for _, disallow := range group.Disallowed {
				if disallow != "" && strings.HasPrefix(reqPath, disallow) {
					return false
				}
			}
		}
	}

	return true
}

type DomainCacheManager struct {
	mu      sync.RWMutex
	cache   map[string]*RobotsData
	expiry  map[string]time.Time
	sfGroup singleflight.Group
}

var GlobalDomainCache = &DomainCacheManager{
	cache:  make(map[string]*RobotsData),
	expiry: make(map[string]time.Time),
}

func (cm *DomainCacheManager) FetchAndCache(domain, rawContent string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.cache[domain] = ParseRobotsTxt(rawContent)
	cm.expiry[domain] = time.Now().Add(24 * time.Hour)
}

func (cm *DomainCacheManager) HasDomainCached(targetURL string) bool {
	reqURL, err := url.Parse(targetURL)
	if err != nil || reqURL.Hostname() == "" {
		return true
	}
	domain := reqURL.Hostname()

	cm.mu.RLock()
	defer cm.mu.RUnlock()
	exp, exists := cm.expiry[domain]
	return exists && time.Now().Before(exp)
}

func (cm *DomainCacheManager) IsDomainAllowed(userAgent, targetURL string) (allowed bool, cached bool) {
	reqURL, err := url.Parse(targetURL)
	if err != nil {
		return true, false
	}
	domain := reqURL.Hostname()
	if domain == "" {
		return true, false
	}

	cm.mu.RLock()
	data, exists := cm.cache[domain]
	exp, _ := cm.expiry[domain]
	cm.mu.RUnlock()

	if !exists || !time.Now().Before(exp) {
		return true, false
	}
	return data.IsAllowed(userAgent, targetURL), true
}

func (cm *DomainCacheManager) EnsureRobotsCached(domain string, fetchFunc func(domain string) (string, error)) (*RobotsData, error) {
	val, err, _ := cm.sfGroup.Do(domain, func() (interface{}, error) {
		cm.mu.RLock()
		data, exists := cm.cache[domain]
		exp, _ := cm.expiry[domain]
		cm.mu.RUnlock()

		if exists && time.Now().Before(exp) {
			return data, nil
		}

		rawContent, err := fetchFunc(domain)
		if err != nil {
			return nil, err
		}

		cm.FetchAndCache(domain, rawContent)
		cm.mu.RLock()
		cachedData := cm.cache[domain]
		cm.mu.RUnlock()
		return cachedData, nil
	})

	if err != nil {
		return nil, err
	}
	return val.(*RobotsData), nil
}

func IsAllowed(userAgent, targetURL string) bool {
	allowed, cached := GlobalDomainCache.IsDomainAllowed(userAgent, targetURL)
	if cached {
		return allowed
	}
	return true
}

// HTTP Client implementation

type Client struct {
	client                  *http.Client
	allowLoopbackForTesting bool
	ProxyManager            ProxyRotator
	RenderEngine            HeadlessRenderer
}

type FetchResult struct {
	Response *http.Response
	FinalURL string
}

func NewClient() *Client {
	return NewTestClientWithTransport(nil, false)
}

func NewClientWithTransport(tr http.RoundTripper) *Client {
	return NewTestClientWithTransport(tr, false)
}

func NewTestClient(allowLoopback bool) *Client {
	return NewTestClientWithTransport(nil, allowLoopback)
}

func NewTestClientWithTransport(tr http.RoundTripper, allowLoopback bool) *Client {
	pm, _ := NewMultiTierProxyManager(nil, nil)
	pm.AllowLoopback = allowLoopback

	c := &Client{
		allowLoopbackForTesting: allowLoopback,
		ProxyManager:            pm,
		RenderEngine:            NewFallbackRenderEngine("chrome"),
	}

	if tr == nil {
		dialer := &net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}

		tr = &http.Transport{
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
					if !c.allowLoopbackForTesting && IsPrivateIP(ip) {
						return nil, fmt.Errorf("blocked request to private/internal IP: %s (%s)", ip.String(), host)
					}
				}

				conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
				if err != nil {
					return nil, err
				}

				if !c.allowLoopbackForTesting {
					remoteIP := getIPFromAddr(conn.RemoteAddr())
					if IsPrivateIP(remoteIP) {
						conn.Close()
						return nil, fmt.Errorf("post-dial blocked request to private/internal IP: %s (%s)", remoteIP.String(), host)
					}
				}

				return conn, nil
			},
		}
	}

	if c.ProxyManager != nil {
		tr = c.ProxyManager.WrapTransport(tr)
	}

	c.client = &http.Client{
		Transport: tr,
		Timeout:   10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects (>10)")
			}
			if req.URL == nil || (req.URL.Scheme != "http" && req.URL.Scheme != "https") {
				return fmt.Errorf("blocked redirect to unsupported scheme: %s", req.URL.Scheme)
			}
			if !c.allowLoopbackForTesting {
				host := req.URL.Hostname()
				if ip := net.ParseIP(host); ip != nil && IsPrivateIP(ip) {
					return fmt.Errorf("blocked redirect to private IP host: %s", host)
				}
			}
			if len(via) > 0 {
				prevHost := via[len(via)-1].URL.Hostname()
				currHost := req.URL.Hostname()
				if !strings.EqualFold(prevHost, currHost) {
					req.Header.Del("Cookie")
					req.Header.Del("Cookie2")
					req.Header.Del("Authorization")
				}
			}
			return nil
		},
	}

	return c
}

func (c *Client) EnsureRobotsCached(ctx context.Context, targetURL string) {
	reqURL, err := url.Parse(targetURL)
	if err != nil || reqURL.Hostname() == "" {
		return
	}
	domain := reqURL.Hostname()

	fetchFunc := func(d string) (string, error) {
		robotsURL := reqURL.Scheme + "://" + reqURL.Host + "/robots.txt"
		req, err := http.NewRequestWithContext(ctx, "GET", robotsURL, nil)
		if err != nil {
			return "", nil
		}
		req.Header.Set("User-Agent", "AntigravityBot/1.0 (+https://example.com/bot)")

		robotsClient := &http.Client{
			Transport: c.client.Transport,
			Timeout:   5 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return http.ErrUseLastResponse
				}
				if len(via) > 0 {
					prevHost := via[len(via)-1].URL.Hostname()
					currHost := req.URL.Hostname()
					if !strings.EqualFold(prevHost, currHost) {
						req.Header.Del("Cookie")
						req.Header.Del("Cookie2")
						req.Header.Del("Authorization")
					}
					if req.URL.Host == via[0].URL.Host {
						return nil
					}
				}
				return http.ErrUseLastResponse
			},
		}

		resp, err := robotsClient.Do(req)
		if err != nil {
			return "", nil
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return "", nil
		}

		limitedBody := io.LimitReader(resp.Body, 1*1024*1024)
		bodyBytes, err := io.ReadAll(limitedBody)
		if err != nil {
			return "", nil
		}

		return string(bodyBytes), nil
	}

	_, _ = GlobalDomainCache.EnsureRobotsCached(domain, fetchFunc)
}

func (c *Client) Fetch(ctx context.Context, targetURL string) (*FetchResult, error) {
	if IsMediaResourceURL(targetURL) {
		return nil, fmt.Errorf("blocked media resource URL: %s", targetURL)
	}

	var lastErr error
	maxRetries := 3
	backoff := 1 * time.Second

	for i := 0; i < maxRetries; i++ {
		if !strings.HasSuffix(targetURL, "/robots.txt") {
			c.EnsureRobotsCached(ctx, targetURL)
			if !IsAllowed("AntigravityBot", targetURL) {
				return nil, fmt.Errorf("crawling disallowed by robots.txt rules for URL: %s", targetURL)
			}
		}

		req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
		if err != nil {
			return nil, err
		}

		profile := GetRotatedHeaderProfile()
		ApplyAntiBotHeaders(req, profile)

		if reqID, ok := ctx.Value(middleware.RequestIDKey).(string); ok && reqID != "" {
			req.Header.Set("X-Request-ID", reqID)
		} else if reqID, ok := ctx.Value("x-request-id").(string); ok && reqID != "" {
			req.Header.Set("X-Request-ID", reqID)
		}

		if span, ok := tracing.FromContext(ctx); ok && span != nil {
			req.Header.Set("traceparent", span.ToW3CHeader())
		} else if traceHeader, ok := ctx.Value("traceparent").(string); ok && traceHeader != "" {
			req.Header.Set("traceparent", traceHeader)
		}

		resp, err := c.client.Do(req)
		if err != nil {
			lastErr = err
			if !sleepWithContext(ctx, backoff) {
				return nil, ctx.Err()
			}
			backoff *= 2
			continue
		}

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

	jitter := time.Duration(50+rand.Intn(100)) * time.Millisecond
	if !sleepWithContext(ctx, jitter) {
		return nil, ctx.Err()
	}

	req, reqErr := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if reqErr != nil {
		return nil, reqErr
	}

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

func (c *Client) FetchSmart(ctx context.Context, targetURL string, forceJS bool) (*FetchResult, error) {
	if !forceJS {
		res, err := c.Fetch(ctx, targetURL)
		if err == nil && res != nil && res.Response != nil {
			limitedBody := io.LimitReader(res.Response.Body, 10*1024*1024)
			htmlBytes, readErr := io.ReadAll(limitedBody)
			res.Response.Body.Close()

			if readErr == nil {
				htmlStr := string(htmlBytes)
				if !IsSPAPlaceholder(htmlStr) {
					res.Response.Body = io.NopCloser(bytes.NewReader(htmlBytes))
					return res, nil
				}
			}
		}
	}

	if c.RenderEngine != nil {
		renderedHTML, renderErr := c.RenderEngine.RenderSPA(ctx, targetURL)
		if renderErr == nil {
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(renderedHTML)),
			}
			resp.Header.Set("Content-Type", "text/html; charset=utf-8")
			return &FetchResult{
				Response: resp,
				FinalURL: targetURL,
			}, nil
		}
	}

	return c.FetchWithStepping(ctx, targetURL)
}
