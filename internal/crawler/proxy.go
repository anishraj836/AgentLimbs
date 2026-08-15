package crawler

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
)

// ProxyTier defines the routing tier levels for proxies.
type ProxyTier int

const (
	Tier1Direct      ProxyTier = 1 // Direct Egress
	Tier2Datacenter  ProxyTier = 2 // Datacenter Proxy
	Tier3Residential ProxyTier = 3 // Residential Proxy
)

// ProxyRotator defines the interface for proxy rotation and routing logic.
type ProxyRotator interface {
	GetProxy(targetURL string, failedAttempts int) (*url.URL, error)
	WrapTransport(baseTr http.RoundTripper) http.RoundTripper
}

// MultiTierProxyManager implements ProxyRotator supporting 3-tier routing.
type MultiTierProxyManager struct {
	mu                 sync.RWMutex
	datacenterProxies  []*url.URL
	residentialProxies []*url.URL
	dcIndex            uint64
	resIndex           uint64
	AllowLoopback      bool
}

// NewMultiTierProxyManager creates a new MultiTierProxyManager instance.
func NewMultiTierProxyManager(dcProxyURLs, resProxyURLs []string) (*MultiTierProxyManager, error) {
	m := &MultiTierProxyManager{
		datacenterProxies:  make([]*url.URL, 0, len(dcProxyURLs)),
		residentialProxies: make([]*url.URL, 0, len(resProxyURLs)),
	}

	for _, pStr := range dcProxyURLs {
		if strings.TrimSpace(pStr) == "" {
			continue
		}
		u, err := url.Parse(pStr)
		if err != nil {
			return nil, fmt.Errorf("invalid datacenter proxy URL %q: %w", pStr, err)
		}
		m.datacenterProxies = append(m.datacenterProxies, u)
	}

	for _, pStr := range resProxyURLs {
		if strings.TrimSpace(pStr) == "" {
			continue
		}
		u, err := url.Parse(pStr)
		if err != nil {
			return nil, fmt.Errorf("invalid residential proxy URL %q: %w", pStr, err)
		}
		m.residentialProxies = append(m.residentialProxies, u)
	}

	return m, nil
}

// NewMultiTierProxyManagerFromEnv initializes a MultiTierProxyManager loading proxies from standard environment variables:
// Tier 2 Datacenter: DATACENTER_PROXY_URLS, PROXY_URL_TIER2
// Tier 3 Residential: RESIDENTIAL_PROXY_URLS, BRIGHTDATA_PROXY_URL, SMARTPROXY_URL, PROXY_URL_TIER3
func NewMultiTierProxyManagerFromEnv() (*MultiTierProxyManager, error) {
	var dcProxies []string
	var resProxies []string

	for _, envKey := range []string{"DATACENTER_PROXY_URLS", "PROXY_URL_TIER2"} {
		if val := os.Getenv(envKey); val != "" {
			for _, p := range strings.Split(val, ",") {
				if trimmed := strings.TrimSpace(p); trimmed != "" {
					dcProxies = append(dcProxies, trimmed)
				}
			}
		}
	}

	for _, envKey := range []string{"RESIDENTIAL_PROXY_URLS", "BRIGHTDATA_PROXY_URL", "SMARTPROXY_URL", "PROXY_URL_TIER3"} {
		if val := os.Getenv(envKey); val != "" {
			for _, p := range strings.Split(val, ",") {
				if trimmed := strings.TrimSpace(p); trimmed != "" {
					resProxies = append(resProxies, trimmed)
				}
			}
		}
	}

	return NewMultiTierProxyManager(dcProxies, resProxies)
}

// GetProxy selects appropriate proxy based on targetURL and failedAttempts count (e.g. retries after 403/429).
// Tier 1 (Direct Egress): failedAttempts == 0 -> returns (nil, nil)
// Tier 2 (Datacenter Proxy): failedAttempts == 1 -> returns next available datacenter proxy
// Tier 3 (Residential Proxy): failedAttempts >= 2 -> returns next available residential proxy
// If a tier has no proxies configured, it falls back to the available tier or direct egress.
func (m *MultiTierProxyManager) GetProxy(targetURL string, failedAttempts int) (*url.URL, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if failedAttempts <= 0 {
		return nil, nil // Tier 1: Direct Egress
	}

	if failedAttempts == 1 {
		// Tier 2: Datacenter Proxy
		if len(m.datacenterProxies) > 0 {
			proxy := m.datacenterProxies[m.dcIndex%uint64(len(m.datacenterProxies))]
			m.dcIndex++
			return proxy, nil
		}
		// Fallback to Tier 3 if no DC proxy
		if len(m.residentialProxies) > 0 {
			proxy := m.residentialProxies[m.resIndex%uint64(len(m.residentialProxies))]
			m.resIndex++
			return proxy, nil
		}
		return nil, nil
	}

	// Tier 3: Residential Proxy (failedAttempts >= 2)
	if len(m.residentialProxies) > 0 {
		proxy := m.residentialProxies[m.resIndex%uint64(len(m.residentialProxies))]
		m.resIndex++
		return proxy, nil
	}
	// Fallback to Tier 2 if no Residential proxy
	if len(m.datacenterProxies) > 0 {
		proxy := m.datacenterProxies[m.dcIndex%uint64(len(m.datacenterProxies))]
		m.dcIndex++
		return proxy, nil
	}

	return nil, nil
}

// ProxyTransport wraps an http.RoundTripper to handle dynamic proxy selection, SSRF checks, and response payload capping.
type ProxyTransport struct {
	mu               sync.RWMutex
	baseTransport    http.RoundTripper
	proxyManager     *MultiTierProxyManager
	cachedTransports map[string]*http.Transport
}

// WrapTransport wraps baseTr with proxy routing and 2MB response capping logic.
func (m *MultiTierProxyManager) WrapTransport(baseTr http.RoundTripper) http.RoundTripper {
	if baseTr == nil {
		baseTr = http.DefaultTransport
	}
	return &ProxyTransport{
		baseTransport:    baseTr,
		proxyManager:     m,
		cachedTransports: make(map[string]*http.Transport),
	}
}

func (pt *ProxyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	failedAttempts := 0
	if val := req.Context().Value(retryAttemptKey); val != nil {
		if attempts, ok := val.(int); ok {
			failedAttempts = attempts
		}
	}

	proxyURL, err := pt.proxyManager.GetProxy(req.URL.String(), failedAttempts)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve proxy: %w", err)
	}

	currentReq := req
	if proxyURL != nil {
		// Clone request to inject Proxy header or transport context if needed
		currentReq = req.Clone(req.Context())
	}

	var tr http.RoundTripper = pt.baseTransport

	// If a proxy URL is assigned, reuse cached Transport to preserve connection pooling
	if proxyURL != nil {
		if stdTr, ok := pt.baseTransport.(*http.Transport); ok {
			proxyKey := proxyURL.String()
			pt.mu.RLock()
			cachedTr, exists := pt.cachedTransports[proxyKey]
			pt.mu.RUnlock()

			if exists {
				tr = cachedTr
			} else {
				pt.mu.Lock()
				cachedTr, exists = pt.cachedTransports[proxyKey]
				if !exists {
					cachedTr = stdTr.Clone()
					cachedTr.Proxy = http.ProxyURL(proxyURL)
					pt.cachedTransports[proxyKey] = cachedTr
				}
				pt.mu.Unlock()
				tr = cachedTr
			}
		}
	}

	resp, err := tr.RoundTrip(currentReq)
	if err != nil {
		return nil, err
	}

	// Enforce SSRF validation on response remote address if connection IP is accessible
	if !pt.proxyManager.AllowLoopback && resp != nil && resp.Request != nil && resp.Request.URL != nil {
		// Verify destination isn't private IP if direct egress
		if proxyURL == nil {
			host := resp.Request.URL.Hostname()
			ips, lookupErr := net.LookupIP(host)
			if lookupErr == nil {
				for _, ip := range ips {
					if IsPrivateIP(ip) {
						resp.Body.Close()
						return nil, fmt.Errorf("SSRF blocked response from internal IP: %s (%s)", ip.String(), host)
					}
				}
			}
		}
	}

	// Enforce 2MB payload cap via io.LimitReader on proxy responses to control proxy bandwidth costs.
	if resp != nil && resp.Body != nil {
		resp.Body = &limitReadCloser{
			reader: io.LimitReader(resp.Body, 2*1024*1024), // 2MB payload cap
			closer: resp.Body,
		}
	}

	return resp, nil
}

type limitReadCloser struct {
	reader io.Reader
	closer io.Closer
}

func (l *limitReadCloser) Read(p []byte) (n int, err error) {
	return l.reader.Read(p)
}

func (l *limitReadCloser) Close() error {
	return l.closer.Close()
}

type contextKey string

const retryAttemptKey contextKey = "retry_attempt_count"

// WithRetryAttempt attaches failed retry attempt count to context.
func WithRetryAttempt(ctx context.Context, failedAttempts int) context.Context {
	return context.WithValue(ctx, retryAttemptKey, failedAttempts)
}
