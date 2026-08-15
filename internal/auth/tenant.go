package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

type contextKey string

const TenantContextKey contextKey = "tenant_context"

type TenantContext struct {
	TenantID string `json:"tenant_id"`
	PlanType string `json:"plan_type"` // e.g. "free", "pro", "enterprise"
	MaxReqs  int    `json:"max_reqs_per_min"`
	Role     string `json:"role"`
}

type JWTClaims struct {
	TenantID  string `json:"tenant_id"`
	PlanType  string `json:"plan_type"`
	Role      string `json:"role"`
	ExpiresAt int64  `json:"exp"`
	IssuedAt  int64  `json:"iat"`
}

// GenerateJWT signs a HS256 JWT token with tenant claims
func GenerateJWT(tenantID, planType, role string, ttl time.Duration, secret string) (string, error) {
	if secret == "" {
		secret = GetJWTSecret()
	}

	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	headerJSON, _ := json.Marshal(header)
	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)

	now := time.Now().Unix()
	claims := JWTClaims{
		TenantID:  tenantID,
		PlanType:  planType,
		Role:      role,
		ExpiresAt: now + int64(ttl.Seconds()),
		IssuedAt:  now,
	}
	claimsJSON, _ := json.Marshal(claims)
	claimsB64 := base64.RawURLEncoding.EncodeToString(claimsJSON)

	unsignedToken := headerB64 + "." + claimsB64

	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(unsignedToken))
	sig := base64.RawURLEncoding.EncodeToString(h.Sum(nil))

	return unsignedToken + "." + sig, nil
}

// ParseJWT validates and parses a HS256 JWT token string
func ParseJWT(tokenStr, secret string) (*JWTClaims, error) {
	if secret == "" {
		secret = GetJWTSecret()
	}

	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid token format: must contain 3 dot-separated parts")
	}

	unsignedToken := parts[0] + "." + parts[1]
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(unsignedToken))
	expectedSig := base64.RawURLEncoding.EncodeToString(h.Sum(nil))

	if !hmac.Equal([]byte(parts[2]), []byte(expectedSig)) {
		return nil, fmt.Errorf("invalid token signature")
	}

	claimsBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("failed to decode claims: %v", err)
	}

	var claims JWTClaims
	if err := json.Unmarshal(claimsBytes, &claims); err != nil {
		return nil, fmt.Errorf("failed to unmarshal claims: %v", err)
	}

	if time.Now().Unix() > claims.ExpiresAt {
		return nil, fmt.Errorf("jwt token expired")
	}

	return &claims, nil
}

func GetJWTSecret() string {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "agentlimbs_default_production_jwt_secret_change_me"
	}
	return secret
}

// TenantQuotaLimiter manages per-tenant token & request rate limits safely
type TenantQuotaLimiter struct {
	mu          sync.Mutex
	requests    map[string]int
	lastReset   time.Time
	tokenCounts map[string]int64
}

var globalLimiter = &TenantQuotaLimiter{
	requests:    make(map[string]int),
	tokenCounts: make(map[string]int64),
	lastReset:   time.Now(),
}

func (l *TenantQuotaLimiter) Allow(tenantID string, maxReqs int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.requests == nil {
		l.requests = make(map[string]int)
	}

	if time.Since(l.lastReset) > time.Minute {
		l.requests = make(map[string]int)
		l.lastReset = time.Now()
	}

	if maxReqs <= 0 {
		maxReqs = 600 // Default 10 req/sec
	}

	if l.requests[tenantID] >= maxReqs {
		return false
	}
	l.requests[tenantID]++
	return true
}

func (l *TenantQuotaLimiter) RecordTokens(tenantID string, tokens int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.tokenCounts == nil {
		l.tokenCounts = make(map[string]int64)
	}
	l.tokenCounts[tenantID] += tokens
}

func (l *TenantQuotaLimiter) GetTokensUsed(tenantID string) int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.tokenCounts == nil {
		return 0
	}
	return l.tokenCounts[tenantID]
}

// TenantMiddleware extracts JWT and injects TenantContext into r.Context()
func TenantMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenantID := "default_tenant"
		planType := "free"
		role := "user"
		maxReqs := 600

		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			tokenStr := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
			claims, err := ParseJWT(tokenStr, GetJWTSecret())
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("WWW-Authenticate", `Bearer realm="WebLimbAI"`)
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(fmt.Sprintf(`{"error":"Unauthorized: Invalid JWT token: %v"}`, err)))
				return
			}
			if claims.TenantID != "" {
				tenantID = claims.TenantID
				planType = claims.PlanType
				role = claims.Role
			}
		}

		if planType == "pro" {
			maxReqs = 3000
		} else if planType == "enterprise" {
			maxReqs = 12000
		}

		if !globalLimiter.Allow(tenantID, maxReqs) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"error":"Tenant rate limit exceeded for tenant '%s' on plan '%s'"}`, tenantID, planType)))
			return
		}

		tc := TenantContext{
			TenantID: tenantID,
			PlanType: planType,
			MaxReqs:  maxReqs,
			Role:     role,
		}

		ctx := context.WithValue(r.Context(), TenantContextKey, tc)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func GetTenantFromContext(ctx context.Context) (TenantContext, bool) {
	tc, ok := ctx.Value(TenantContextKey).(TenantContext)
	if !ok {
		return TenantContext{TenantID: "default_tenant", PlanType: "free", Role: "user", MaxReqs: 600}, false
	}
	return tc, true
}
