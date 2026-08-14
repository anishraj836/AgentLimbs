package tracing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStartSpanAndW3CHeader(t *testing.T) {
	ctx := context.Background()
	span, ctx := StartSpan(ctx, "test_operation")
	defer span.End()

	if span.Name != "test_operation" {
		t.Errorf("expected span name 'test_operation', got '%s'", span.Name)
	}

	if len(span.TraceID) != 32 {
		t.Errorf("expected 32-char hex TraceID, got '%s'", span.TraceID)
	}

	if len(span.SpanID) != 16 {
		t.Errorf("expected 16-char hex SpanID, got '%s'", span.SpanID)
	}

	w3c := span.ToW3CHeader()
	if len(w3c) < 50 {
		t.Errorf("invalid W3C header format: %s", w3c)
	}

	childSpan, _ := StartSpan(ctx, "child_operation")
	defer childSpan.End()

	if childSpan.TraceID != span.TraceID {
		t.Errorf("child traceID should match parent traceID '%s', got '%s'", span.TraceID, childSpan.TraceID)
	}

	if childSpan.ParentID != span.SpanID {
		t.Errorf("child parentID should match parent spanID '%s', got '%s'", span.SpanID, childSpan.ParentID)
	}
}

func TestTracingMiddleware(t *testing.T) {
	handler := TracingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		span, ok := FromContext(r.Context())
		if !ok {
			t.Fatal("expected span in context, got nil")
		}
		if span.Tags["http.method"] != "GET" {
			t.Errorf("expected http.method GET, got %s", span.Tags["http.method"])
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/v1/search?q=test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected HTTP 200 OK, got %d", rr.Code)
	}

	if rr.Header().Get(TraceHeader) == "" {
		t.Error("expected traceparent response header, got empty")
	}
}
