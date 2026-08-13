package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestIDMiddleware(t *testing.T) {
	handler := RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID, ok := r.Context().Value(RequestIDKey).(string)
		if !ok || reqID == "" {
			t.Errorf("RequestID not found in context")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Header().Get("X-Request-ID") == "" {
		t.Errorf("Expected X-Request-ID header to be set")
	}

	req2 := httptest.NewRequest("GET", "/", nil)
	req2.Header.Set("X-Request-ID", "test-id")
	rr2 := httptest.NewRecorder()

	handler.ServeHTTP(rr2, req2)

	if rr2.Header().Get("X-Request-ID") != "test-id" {
		t.Errorf("Expected X-Request-ID header to match input: got %v", rr2.Header().Get("X-Request-ID"))
	}
}
