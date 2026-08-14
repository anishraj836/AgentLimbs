package ratelimit

import (
	"net"
	"net/http"
	"sync"
	"time"
)

type TokenBucket struct {
	rate       float64 // tokens per second
	capacity   float64 // max tokens
	tokens     float64
	lastUpdate time.Time
	mu         sync.Mutex
}

func NewTokenBucket(rate float64, capacity float64) *TokenBucket {
	return &TokenBucket{
		rate:       rate,
		capacity:   capacity,
		tokens:     capacity,
		lastUpdate: time.Now(),
	}
}

func (tb *TokenBucket) Allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.lastUpdate).Seconds()
	tb.lastUpdate = now

	tb.tokens += elapsed * tb.rate
	if tb.tokens > tb.capacity {
		tb.tokens = tb.capacity
	}

	if tb.tokens >= 1.0 {
		tb.tokens -= 1.0
		return true
	}
	return false
}

// KeyedRateLimiter manages separate token buckets per client key (e.g. IP address or API key).
type KeyedRateLimiter struct {
	rate     float64
	capacity float64
	buckets  map[string]*TokenBucket
	mu       sync.Mutex
}

func NewKeyedRateLimiter(rate float64, capacity float64) *KeyedRateLimiter {
	return &KeyedRateLimiter{
		rate:     rate,
		capacity: capacity,
		buckets:  make(map[string]*TokenBucket),
	}
}

func (k *KeyedRateLimiter) Allow(key string) bool {
	k.mu.Lock()
	tb, exists := k.buckets[key]
	if !exists {
		tb = NewTokenBucket(k.rate, k.capacity)
		k.buckets[key] = tb
	}
	k.mu.Unlock()

	return tb.Allow()
}

// RateLimiterMiddleware returns an HTTP handler middleware enforcing token bucket rate limits per client IP / Key.
func RateLimiterMiddleware(rate float64, capacity float64) func(http.Handler) http.Handler {
	limiter := NewKeyedRateLimiter(rate, capacity)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			clientKey := r.Header.Get("X-API-Key")
			if clientKey == "" {
				host, _, err := net.SplitHostPort(r.RemoteAddr)
				if err == nil && host != "" {
					clientKey = host
				} else {
					clientKey = r.RemoteAddr
				}
			}

			if !limiter.Allow(clientKey) {
				w.Header().Set("Retry-After", "1")
				http.Error(w, `{"error":"Too Many Requests","message":"Rate limit exceeded for client. Please retry after a brief delay."}`, http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
