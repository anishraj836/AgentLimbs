package tracing

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"sync"
	"time"
)

type spanContextKey string

const (
	SpanContextKey spanContextKey = "otel_span"
	TraceHeader    string         = "traceparent"
)

type Span struct {
	TraceID   string            `json:"trace_id"`
	SpanID    string            `json:"span_id"`
	ParentID  string            `json:"parent_id,omitempty"`
	Name      string            `json:"name"`
	StartTime time.Time         `json:"start_time"`
	EndTime   time.Time         `json:"end_time"`
	Tags      map[string]string `json:"tags"`
	mu        sync.RWMutex
}

func randomHex(bytesLen int) string {
	b := make([]byte, bytesLen)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func StartSpan(ctx context.Context, name string) (*Span, context.Context) {
	traceID := randomHex(16) // 128-bit W3C Trace ID
	spanID := randomHex(8)   // 64-bit W3C Span ID
	parentID := ""

	if parent, ok := FromContext(ctx); ok {
		parent.mu.RLock()
		traceID = parent.TraceID
		parentID = parent.SpanID
		parent.mu.RUnlock()
	}

	s := &Span{
		TraceID:   traceID,
		SpanID:    spanID,
		ParentID:  parentID,
		Name:      name,
		StartTime: time.Now(),
		Tags:      make(map[string]string),
	}

	newCtx := context.WithValue(ctx, SpanContextKey, s)
	return s, newCtx
}

func (s *Span) SetIDs(traceID, parentID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if traceID != "" {
		s.TraceID = traceID
	}
	if parentID != "" {
		s.ParentID = parentID
	}
}

func (s *Span) SetTag(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Tags[key] = value
}

func (s *Span) End() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.EndTime = time.Now()
}

func (s *Span) Duration() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.EndTime.IsZero() {
		return time.Since(s.StartTime)
	}
	return s.EndTime.Sub(s.StartTime)
}

// ToW3CHeader returns W3C traceparent string: 00-traceid-spanid-01
func (s *Span) ToW3CHeader() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return fmt.Sprintf("00-%s-%s-01", s.TraceID, s.SpanID)
}

func FromContext(ctx context.Context) (*Span, bool) {
	s, ok := ctx.Value(SpanContextKey).(*Span)
	return s, ok
}

// TracingMiddleware extracts W3C traceparent header or starts a new trace
func TracingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := fmt.Sprintf("%s %s", r.Method, r.URL.Path)
		span, ctx := StartSpan(r.Context(), name)

		if tp := r.Header.Get(TraceHeader); tp != "" {
			parts := stringsSplit(tp, "-")
			if len(parts) >= 3 && len(parts[1]) == 32 && len(parts[2]) == 16 {
				span.SetIDs(parts[1], parts[2])
			}
		}

		span.SetTag("http.method", r.Method)
		span.SetTag("http.url", r.URL.String())
		span.SetTag("http.remote_addr", r.RemoteAddr)

		w.Header().Set(TraceHeader, span.ToW3CHeader())

		defer func() {
			span.End()
		}()

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func stringsSplit(s, sep string) []string {
	var res []string
	start := 0
	for i := 0; i < len(s); i++ {
		if i+len(sep) <= len(s) && s[i:i+len(sep)] == sep {
			res = append(res, s[start:i])
			start = i + len(sep)
			i += len(sep) - 1
		}
	}
	res = append(res, s[start:])
	return res
}
