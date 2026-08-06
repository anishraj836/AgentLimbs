package webhook

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestWebhookDispatcher(t *testing.T) {
	var count int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&count, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	dispatcher := NewWebhookDispatcher()
	dispatcher.RegisterEndpoint(server.URL)

	ctx := context.Background()
	dispatcher.DispatchEvent(ctx, "page_indexed", map[string]string{
		"url": "https://golang.org",
	})

	time.Sleep(100 * time.Millisecond)

	if atomic.LoadInt32(&count) != 1 {
		t.Errorf("expected 1 webhook delivery, got %d", atomic.LoadInt32(&count))
	}
}
