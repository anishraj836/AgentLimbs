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

	// 4. Cloud mode with blank server apiKey -> Should return 401 Unauthorized for any request
	cloudHandler := SecurityMiddleware("cloud", "", limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req4 := httptest.NewRequest("GET", "/v1/scrape", nil)
	rr4 := httptest.NewRecorder()
	cloudHandler.ServeHTTP(rr4, req4)
	if rr4.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized in cloud mode with unconfigured apiKey, got %d", rr4.Code)
	}

	// 5. Local mode with blank server apiKey -> Should return 200 OK (local dev mode)
	localHandler := SecurityMiddleware("local", "", limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req5 := httptest.NewRequest("GET", "/v1/scrape", nil)
	rr5 := httptest.NewRecorder()
	localHandler.ServeHTTP(rr5, req5)
	if rr5.Code != http.StatusOK {
		t.Fatalf("expected 200 OK in local dev mode without apiKey, got %d", rr5.Code)
	}
}

func TestGetClientIP(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		headers    map[string]string
		expectedIP string
	}{
		{
			name:       "X-Forwarded-For single IP",
			remoteAddr: "10.0.0.1:12345",
			headers:    map[string]string{"X-Forwarded-For": "203.0.113.195"},
			expectedIP: "203.0.113.195",
		},
		{
			name:       "X-Forwarded-For multiple IPs takes leftmost valid IP",
			remoteAddr: "10.0.0.1:12345",
			headers:    map[string]string{"X-Forwarded-For": "203.0.113.195, 70.41.3.18, 150.172.238.178"},
			expectedIP: "203.0.113.195",
		},
		{
			name:       "X-Forwarded-For with spaces and port",
			remoteAddr: "10.0.0.1:12345",
			headers:    map[string]string{"X-Forwarded-For": " 203.0.113.195:8080 , 70.41.3.18 "},
			expectedIP: "203.0.113.195",
		},
		{
			name:       "X-Forwarded-For invalid IP skips to next valid IP",
			remoteAddr: "10.0.0.1:12345",
			headers:    map[string]string{"X-Forwarded-For": "invalid_ip, 203.0.113.195"},
			expectedIP: "203.0.113.195",
		},
		{
			name:       "X-Real-IP fallback when XFF absent",
			remoteAddr: "10.0.0.1:12345",
			headers:    map[string]string{"X-Real-IP": "198.51.100.1"},
			expectedIP: "198.51.100.1",
		},
		{
			name:       "X-Forwarded-For takes priority over X-Real-IP",
			remoteAddr: "10.0.0.1:12345",
			headers:    map[string]string{"X-Forwarded-For": "203.0.113.195", "X-Real-IP": "198.51.100.1"},
			expectedIP: "203.0.113.195",
		},
		{
			name:       "Fallback to RemoteAddr when headers absent",
			remoteAddr: "192.0.2.1:54321",
			headers:    map[string]string{},
			expectedIP: "192.0.2.1",
		},
		{
			name:       "Fallback to RemoteAddr without port when headers absent",
			remoteAddr: "192.0.2.1",
			headers:    map[string]string{},
			expectedIP: "192.0.2.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tt.remoteAddr
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			ip := GetClientIP(req)
			if ip != tt.expectedIP {
				t.Errorf("expected IP %s, got %s", tt.expectedIP, ip)
			}
		})
	}
}

func TestSecurityMiddlewareIPRateLimiting(t *testing.T) {
	limiter := NewRateLimiter(2) // Max 2 requests per minute per IP
	handler := SecurityMiddleware("local", "", limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// IP 1: 203.0.113.1 via X-Forwarded-For
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Forwarded-For", "203.0.113.1")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("request %d for IP1 expected 200, got %d", i+1, rr.Code)
		}
	}

	// 3rd request from IP 1 should be rate limited (429)
	reqExceeded := httptest.NewRequest("GET", "/", nil)
	reqExceeded.Header.Set("X-Forwarded-For", "203.0.113.1")
	rrExceeded := httptest.NewRecorder()
	handler.ServeHTTP(rrExceeded, reqExceeded)
	if rrExceeded.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 Too Many Requests for IP1 on 3rd request, got %d", rrExceeded.Code)
	}

	// Different IP 2 (203.0.113.2) should still be allowed
	reqIP2 := httptest.NewRequest("GET", "/", nil)
	reqIP2.Header.Set("X-Forwarded-For", "203.0.113.2")
	rrIP2 := httptest.NewRecorder()
	handler.ServeHTTP(rrIP2, reqIP2)
	if rrIP2.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for IP2 despite IP1 being rate limited, got %d", rrIP2.Code)
	}
}
