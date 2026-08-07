package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecurityMiddlewareAuthAndRateLimiting(t *testing.T) {
	limiter := NewRateLimiter(5) // Max 5 requests per minute
	apiKey := "test_secret_key_123"

	handler := SecurityMiddleware("local", apiKey, limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))

	// 1. Request without API Key -> Should return 401 Unauthorized
	req1 := httptest.NewRequest("GET", "/v1/scrape", nil)
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized when API key missing, got %d", rr1.Code)
	}

	// 2. Request with wrong API Key -> Should return 401 Unauthorized
	req2 := httptest.NewRequest("GET", "/v1/scrape", nil)
	req2.Header.Set("X-API-Key", "wrong_key")
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized when API key wrong, got %d", rr2.Code)
	}

	// 3. Request with valid API Key -> Should return 200 OK
	req3 := httptest.NewRequest("GET", "/v1/scrape", nil)
	req3.Header.Set("X-API-Key", apiKey)
	rr3 := httptest.NewRecorder()
	handler.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusOK {
		t.Fatalf("expected 200 OK with valid API key, got %d", rr3.Code)
	}
}
