package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/crawler-monorepo/internal/crawler"
)

type WebhookPayload struct {
	Event     string      `json:"event"`
	Timestamp int64       `json:"timestamp"`
	Data      interface{} `json:"data"`
}

type WebhookJob struct {
	URL     string
	Payload []byte
}

type WebhookDispatcher struct {
	mu            sync.RWMutex
	endpoints     []string
	client        *http.Client
	jobChan       chan WebhookJob
	secretKey     string
	allowLoopback bool
	wg            sync.WaitGroup
}

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

func isTestEnv() bool {
	return strings.HasSuffix(os.Args[0], ".test") || os.Getenv("WEBHOOK_ALLOW_LOOPBACK") == "true"
}

func NewWebhookDispatcher() *WebhookDispatcher {
	return NewWebhookDispatcherWithOpts(os.Getenv("WEBHOOK_SECRET"), isTestEnv())
}

func NewWebhookDispatcherWithOpts(secretKey string, allowLoopback bool) *WebhookDispatcher {
	wd := &WebhookDispatcher{
		endpoints:     make([]string, 0),
		jobChan:       make(chan WebhookJob, 1000),
		secretKey:     secretKey,
		allowLoopback: allowLoopback,
	}

	dialer := &net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	transport := &http.Transport{
		MaxIdleConns:        100,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 5 * time.Second,
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
				if ip.IsLoopback() && (wd.allowLoopback || isTestEnv()) {
					continue
				}
				if crawler.IsPrivateIP(ip) {
					return nil, fmt.Errorf("blocked webhook request to private/internal IP: %s (%s)", ip.String(), host)
				}
			}

			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
			if err != nil {
				return nil, err
			}

			remoteIP := getIPFromAddr(conn.RemoteAddr())
			if remoteIP != nil {
				if remoteIP.IsLoopback() && (wd.allowLoopback || isTestEnv()) {
					return conn, nil
				}
				if crawler.IsPrivateIP(remoteIP) {
					conn.Close()
					return nil, fmt.Errorf("post-dial blocked webhook request to private/internal IP: %s (%s)", remoteIP.String(), host)
				}
			}

			return conn, nil
		},
	}

	wd.client = &http.Client{
		Transport: transport,
		Timeout:   5 * time.Second,
	}

	for i := 0; i < 10; i++ {
		wd.wg.Add(1)
		go wd.worker()
	}

	return wd
}

func (wd *WebhookDispatcher) SetSecretKey(secret string) {
	wd.mu.Lock()
	defer wd.mu.Unlock()
	wd.secretKey = secret
}

func (wd *WebhookDispatcher) SetAllowLoopback(allow bool) {
	wd.mu.Lock()
	defer wd.mu.Unlock()
	wd.allowLoopback = allow
}

func (wd *WebhookDispatcher) worker() {
	defer wd.wg.Done()
	for job := range wd.jobChan {
		func(j WebhookJob) {
			defer func() {
				if r := recover(); r != nil {
					// isolate panic
				}
			}()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, "POST", j.URL, bytes.NewReader(j.Payload))
			if err != nil {
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("User-Agent", "AgentLimbs-Webhook/1.0")

			wd.mu.RLock()
			secret := wd.secretKey
			wd.mu.RUnlock()

			if secret != "" {
				mac := hmac.New(sha256.New, []byte(secret))
				mac.Write(j.Payload)
				sig := hex.EncodeToString(mac.Sum(nil))
				req.Header.Set("X-Hub-Signature-256", "sha256="+sig)
			}

			resp, err := wd.client.Do(req)
			if err == nil && resp != nil {
				resp.Body.Close()
			}
		}(job)
	}
}

// Close gracefully shuts down the dispatcher and waits for workers to exit.
func (wd *WebhookDispatcher) Close() {
	close(wd.jobChan)
	wd.wg.Wait()
}

// RegisterEndpoint registers an HTTP webhook URL for push event notifications.
func (wd *WebhookDispatcher) RegisterEndpoint(targetURL string) error {
	targetURL = strings.TrimSpace(targetURL)
	parsed, err := url.Parse(targetURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("invalid webhook URL: %s (must be http or https)", targetURL)
	}

	wd.mu.Lock()
	defer wd.mu.Unlock()

	for _, ep := range wd.endpoints {
		if ep == targetURL {
			return nil
		}
	}
	wd.endpoints = append(wd.endpoints, targetURL)
	return nil
}

// DispatchEvent asynchronously pushes event payloads to all registered webhook endpoints.
func (wd *WebhookDispatcher) DispatchEvent(ctx context.Context, eventName string, data interface{}) {
	wd.mu.RLock()
	endpointsCopy := make([]string, len(wd.endpoints))
	copy(endpointsCopy, wd.endpoints)
	wd.mu.RUnlock()

	if len(endpointsCopy) == 0 {
		return
	}

	payload := WebhookPayload{
		Event:     eventName,
		Timestamp: time.Now().Unix(),
		Data:      data,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return
	}

	for _, endpoint := range endpointsCopy {
		select {
		case wd.jobChan <- WebhookJob{URL: endpoint, Payload: bodyBytes}:
		default:
			// Non-blocking select write: dropped if queue full
		}
	}
}
