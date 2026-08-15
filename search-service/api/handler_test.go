package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/crawler-monorepo/internal/index"
)

func TestSearchHealthEndpoints(t *testing.T) {
	searchEngine := index.NewEngine()
	handler := NewSearchHandler(searchEngine)

	// 1. /health
	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()
	handler.Health(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 from Health, got %d", rr.Code)
	}

	// 2. /healthz
	req = httptest.NewRequest("GET", "/healthz", nil)
	rr = httptest.NewRecorder()
	handler.Healthz(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 from Healthz, got %d", rr.Code)
	}

	// 3. /livez
	req = httptest.NewRequest("GET", "/livez", nil)
	rr = httptest.NewRecorder()
	handler.Livez(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 from Livez, got %d", rr.Code)
	}

	// 4. /readyz
	req = httptest.NewRequest("GET", "/readyz", nil)
	rr = httptest.NewRecorder()
	handler.Readyz(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 from Readyz, got %d", rr.Code)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["status"] != "ready" {
		t.Errorf("expected status 'ready', got %v", body["status"])
	}
}

func TestCheckAuth_SHA256Protection(t *testing.T) {
	t.Setenv("AGENT_API_KEY", "super_secret_agent_key_456")

	searchEngine := index.NewEngine()
	handler := NewSearchHandler(searchEngine)

	// 1. Missing key -> 401
	req1 := httptest.NewRequest("GET", "/stats", nil)
	rr1 := httptest.NewRecorder()
	handler.Stats(rr1, req1)
	if rr1.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for missing API key, got %d", rr1.Code)
	}

	// 2. Wrong length key -> 401
	req2 := httptest.NewRequest("GET", "/stats", nil)
	req2.Header.Set("X-API-Key", "short")
	rr2 := httptest.NewRecorder()
	handler.Stats(rr2, req2)
	if rr2.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for wrong length API key, got %d", rr2.Code)
	}

	// 3. Same length wrong key -> 401
	req3 := httptest.NewRequest("GET", "/stats", nil)
	req3.Header.Set("X-API-Key", "super_secret_agent_key_999")
	rr3 := httptest.NewRecorder()
	handler.Stats(rr3, req3)
	if rr3.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for same length wrong API key, got %d", rr3.Code)
	}

	// 4. Valid key via X-API-Key header -> 200
	req4 := httptest.NewRequest("GET", "/stats", nil)
	req4.Header.Set("X-API-Key", "super_secret_agent_key_456")
	rr4 := httptest.NewRecorder()
	handler.Stats(rr4, req4)
	if rr4.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for valid API key in header, got %d", rr4.Code)
	}

	// 5. Valid key via Bearer token -> 200
	req5 := httptest.NewRequest("GET", "/stats", nil)
	req5.Header.Set("Authorization", "Bearer super_secret_agent_key_456")
	rr5 := httptest.NewRecorder()
	handler.Stats(rr5, req5)
	if rr5.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for valid API key in Bearer token, got %d", rr5.Code)
	}

	// 6. Valid key via Query param -> 200
	req6 := httptest.NewRequest("GET", "/stats?api_key=super_secret_agent_key_456", nil)
	rr6 := httptest.NewRecorder()
	handler.Stats(rr6, req6)
	if rr6.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for valid API key in query param, got %d", rr6.Code)
	}
}

