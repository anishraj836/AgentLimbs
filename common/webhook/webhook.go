package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

type WebhookPayload struct {
	Event     string      `json:"event"`
	Timestamp int64       `json:"timestamp"`
	Data      interface{} `json:"data"`
}

type WebhookDispatcher struct {
	mu        sync.RWMutex
	endpoints []string
	client    *http.Client
}

func NewWebhookDispatcher() *WebhookDispatcher {
	return &WebhookDispatcher{
		endpoints: make([]string, 0),
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// RegisterEndpoint registers an HTTP webhook URL for push event notifications.
func (wd *WebhookDispatcher) RegisterEndpoint(targetURL string) {
	wd.mu.Lock()
	defer wd.mu.Unlock()
	wd.endpoints = append(wd.endpoints, targetURL)
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
		go func(url string) {
			req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
			if err != nil {
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("User-Agent", "AgentLimbs-Webhook/1.0")

			resp, err := wd.client.Do(req)
			if err == nil && resp != nil {
				resp.Body.Close()
			}
		}(endpoint)
	}
}
