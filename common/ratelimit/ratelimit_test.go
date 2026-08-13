package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTokenBucket(t *testing.T) {
	tb := NewTokenBucket(10.0, 2.0)

	if !tb.Allow() {
		t.Error("expected first request to be allowed")
	}
	if !tb.Allow() {
		t.Error("expected second request to be allowed (within capacity)")
	}
	if tb.Allow() {
		t.Error("expected third request to be rate limited (capacity exhausted)")
	}
}

func TestRateLimiterMiddleware(t *testing.T) {
	mw := RateLimiterMiddleware(10.0, 1.0)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/v1/scrape", nil)

	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req)
	if w1.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", w1.Code)
	}

	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req)
	if w2.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 Too Many Requests, got %d", w2.Code)
	}
}
