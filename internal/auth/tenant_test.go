package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestJWTGenerateAndParse(t *testing.T) {
	secret := "test_secret_12345"
	token, err := GenerateJWT("tenant_acme", "pro", "admin", 1*time.Hour, secret)
	if err != nil {
		t.Fatalf("GenerateJWT failed: %v", err)
	}

	claims, err := ParseJWT(token, secret)
	if err != nil {
		t.Fatalf("ParseJWT failed: %v", err)
	}

	if claims.TenantID != "tenant_acme" {
		t.Errorf("expected tenant_id 'tenant_acme', got '%s'", claims.TenantID)
	}
	if claims.PlanType != "pro" {
		t.Errorf("expected plan_type 'pro', got '%s'", claims.PlanType)
	}
	if claims.Role != "admin" {
		t.Errorf("expected role 'admin', got '%s'", claims.Role)
	}
}

func TestJWTExpiredToken(t *testing.T) {
	secret := "test_secret_12345"
	token, err := GenerateJWT("tenant_acme", "pro", "admin", -1*time.Second, secret)
	if err != nil {
		t.Fatalf("GenerateJWT failed: %v", err)
	}

	_, err = ParseJWT(token, secret)
	if err == nil {
		t.Error("expected error for expired JWT token, got nil")
	}
}

func TestTenantMiddleware(t *testing.T) {
	secret := GetJWTSecret()
	token, _ := GenerateJWT("tenant_corp", "enterprise", "member", 1*time.Hour, secret)

	handler := TenantMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tc, ok := GetTenantFromContext(r.Context())
		if !ok || tc.TenantID != "tenant_corp" {
			t.Errorf("expected tenant_id 'tenant_corp', got '%s' (ok=%v)", tc.TenantID, ok)
		}
		if tc.PlanType != "enterprise" {
			t.Errorf("expected plan 'enterprise', got '%s'", tc.PlanType)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/v1/search", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected HTTP 200 OK, got %d", rr.Code)
	}
}

func TestTenantQuotaLimiter(t *testing.T) {
	limiter := &TenantQuotaLimiter{
		requests:    make(map[string]int),
		tokenCounts: make(map[string]int64),
		lastReset:   time.Now(),
	}

	if !limiter.Allow("tenant_a", 2) {
		t.Error("expected first request to be allowed")
	}
	if !limiter.Allow("tenant_a", 2) {
		t.Error("expected second request to be allowed")
	}
	if limiter.Allow("tenant_a", 2) {
		t.Error("expected third request to be blocked by rate limit")
	}

	limiter.RecordTokens("tenant_a", 1500)
	if limiter.GetTokensUsed("tenant_a") != 1500 {
		t.Errorf("expected 1500 tokens recorded, got %d", limiter.GetTokensUsed("tenant_a"))
	}

	// Advance time past 1 minute window reset
	limiter.lastReset = time.Now().Add(-2 * time.Minute)

	// Call Allow which triggers window reset
	if !limiter.Allow("tenant_a", 2) {
		t.Error("expected request to be allowed after rate limit reset")
	}

	// Verify token count was NOT wiped
	if limiter.GetTokensUsed("tenant_a") != 1500 {
		t.Errorf("expected token count 1500 to be preserved across window reset, got %d", limiter.GetTokensUsed("tenant_a"))
	}
}

func TestTenantMiddleware_UnauthenticatedIgnoresXTenantID(t *testing.T) {
	calledNext := false
	handler := TenantMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calledNext = true
		tc, ok := GetTenantFromContext(r.Context())
		if !ok {
			t.Fatalf("expected TenantContext to be set in context")
		}
		if tc.TenantID != "default_tenant" {
			t.Errorf("expected tenant_id 'default_tenant', got '%s'", tc.TenantID)
		}
		if tc.PlanType != "free" {
			t.Errorf("expected plan_type 'free', got '%s'", tc.PlanType)
		}
		if tc.Role != "user" {
			t.Errorf("expected role 'user', got '%s'", tc.Role)
		}
		if tc.MaxReqs != 600 {
			t.Errorf("expected maxReqs 600, got %d", tc.MaxReqs)
		}
		w.WriteHeader(http.StatusOK)
	}))

	// Attempt impersonation via X-Tenant-ID header without a valid JWT
	req := httptest.NewRequest("GET", "/v1/search", nil)
	req.Header.Set("X-Tenant-ID", "attacker_impersonated_tenant")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if !calledNext {
		t.Fatalf("expected downstream handler to be called for unauthenticated request")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("expected HTTP 200 OK, got %d", rr.Code)
	}
}

func TestTenantMiddleware_InvalidJWTReturnsUnauthorized(t *testing.T) {
	handler := TenantMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Invalid JWT token
	req := httptest.NewRequest("GET", "/v1/search", nil)
	req.Header.Set("Authorization", "Bearer invalid.jwt.token")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected HTTP 401 Unauthorized for invalid JWT, got %d", rr.Code)
	}
}
