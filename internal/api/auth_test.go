// Package api — auth_test.go
//
// Unit + HTTP tests for authentication (username/password session tokens) and
// authorization (role-gated routes). All hermetic — no database required.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// ─── Roles & authorization ────────────────────────────────────────────────────

func TestRoleHierarchy(t *testing.T) {
	analyst := Principal{Username: "a", Role: RoleAnalyst}
	admin := Principal{Username: "d", Role: RoleAdmin}

	if analyst.AtLeast(RoleAdmin) {
		t.Error("analyst must not satisfy admin")
	}
	if !admin.AtLeast(RoleAdmin) || !admin.AtLeast(RoleAnalyst) {
		t.Error("admin must satisfy admin and analyst")
	}
	if !analyst.AtLeast(RoleAnalyst) {
		t.Error("analyst must satisfy analyst")
	}
	if Role("nonsense").Valid() || Role("viewer").Valid() {
		t.Error("unknown/removed roles must not be valid")
	}
}

func TestRequireRole_EnforcesMinimum(t *testing.T) {
	reached := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})
	guarded := RequireRole(RoleAdmin)(next)

	// No principal → 401.
	reached = false
	rec := httptest.NewRecorder()
	guarded.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/audit", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no principal: got %d, want 401", rec.Code)
	}
	if reached {
		t.Error("handler must not run without a principal")
	}

	// Analyst principal → 403.
	reached = false
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/audit", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyPrincipal,
		Principal{Username: "a", Role: RoleAnalyst}))
	guarded.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("analyst: got %d, want 403", rec.Code)
	}
	if reached {
		t.Error("handler must not run for insufficient role")
	}

	// Admin principal → 200.
	reached = false
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/audit", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyPrincipal,
		Principal{Username: "d", Role: RoleAdmin}))
	guarded.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !reached {
		t.Errorf("admin: got %d reached=%v, want 200 true", rec.Code, reached)
	}
}

// ─── User store ───────────────────────────────────────────────────────────────

func TestNewUserStore_ParsesAndValidates(t *testing.T) {
	s, err := NewUserStore("admin:secret:admin,ana:pw:analyst")
	if err != nil {
		t.Fatalf("NewUserStore: %v", err)
	}
	if len(s.users) != 2 {
		t.Fatalf("got %d users, want 2", len(s.users))
	}

	if got, ok := s.Authenticate("ana", "pw"); !ok || got.Role != RoleAnalyst {
		t.Errorf("authenticate ana: ok=%v role=%v", ok, got.Role)
	}
	if _, ok := s.Authenticate("ana", "wrong"); ok {
		t.Error("wrong password must fail")
	}
	if _, ok := s.Authenticate("ghost", "pw"); ok {
		t.Error("unknown user must fail")
	}
}

func TestNewUserStore_RejectsBadSpecs(t *testing.T) {
	for _, spec := range []string{
		"admin:only-two-fields",
		"admin:secret:superuser",
		"x:pw:viewer", // viewer role was removed
		":pw:admin",
		"admin::admin",
	} {
		if _, err := NewUserStore(spec); err == nil {
			t.Errorf("spec %q should be rejected", spec)
		}
	}
}

func TestNewUserStore_EmptyFallsBackToDefault(t *testing.T) {
	s, err := NewUserStore("")
	if err != nil {
		t.Fatalf("NewUserStore(\"\"): %v", err)
	}
	if _, ok := s.Authenticate("admin", "admin"); !ok {
		t.Error("default admin/admin credentials should work")
	}
	if !s.UsesDefaultCredentials() {
		t.Error("default credentials must be flagged")
	}
}

// ─── Session tokens ───────────────────────────────────────────────────────────

func TestSessionToken_RoundTrip(t *testing.T) {
	signer := NewSessionSigner("test-secret", time.Hour)
	p := Principal{Username: "alice", Role: RoleAnalyst}

	token, exp := signer.Sign(p, time.Now())
	if token == "" || !strings.Contains(token, ".") {
		t.Fatalf("malformed token: %q", token)
	}
	if time.Until(exp) < 30*time.Minute {
		t.Errorf("expiry too soon: %v", exp)
	}

	got, err := signer.Verify(token, time.Now())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got != p {
		t.Errorf("round-trip principal = %+v, want %+v", got, p)
	}
}

func TestSessionToken_RejectsTamperedAndExpired(t *testing.T) {
	signer := NewSessionSigner("test-secret", time.Hour)
	token, _ := signer.Sign(Principal{Username: "bob", Role: RoleAnalyst}, time.Now())

	// Tampered payload with a valid-looking signature structure.
	parts := strings.SplitN(token, ".", 2)
	if _, err := signer.Verify(parts[0]+"x."+parts[1], time.Now()); err == nil {
		t.Error("tampered token must fail")
	}
	// Wrong secret.
	other := NewSessionSigner("different-secret", time.Hour)
	if _, err := other.Verify(token, time.Now()); err == nil {
		t.Error("token signed with another secret must fail")
	}
	// Malformed.
	if _, err := signer.Verify("not-a-token", time.Now()); err == nil {
		t.Error("malformed token must fail")
	}
	// Expired: sign in the past.
	expired, _ := signer.Sign(Principal{Username: "bob", Role: RoleAnalyst}, time.Now().Add(-2*time.Hour))
	if _, err := signer.Verify(expired, time.Now()); err == nil {
		t.Fatal("expired token must fail")
	} else if !strings.Contains(strings.ToLower(err.Error()), "expired") {
		t.Errorf("expiry error must mention 'expired', got %q", err)
	}
}

// ─── End-to-end HTTP: login → authorized request ──────────────────────────────

func probeRouter(t *testing.T) http.Handler {
	t.Helper()
	users, err := NewUserStore("admin:adminpw:admin,ana:anapw:analyst")
	if err != nil {
		t.Fatal(err)
	}
	signer := NewSessionSigner("test-secret", time.Hour)
	authH := NewAuthHandler(NewEnvCredentials(users), signer, nil)

	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/login", authH.Login)
		r.Group(func(r chi.Router) {
			r.Use(AuthMiddleware(NewSessionVerifier(signer)))
			r.Use(RequireRole(RoleAnalyst))
			r.Get("/auth/me", authH.Me)
			r.Get("/read", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
			r.Post("/write", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
			r.With(RequireRole(RoleAdmin)).Get("/audit", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
		})
	})
	return r
}

func login(t *testing.T, h http.Handler, username, password string) (int, string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(string(body)))
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		return rec.Code, ""
	}
	var env struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	return rec.Code, env.Data.Token
}

func do(h http.Handler, method, path, token string) int {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	h.ServeHTTP(rec, req)
	return rec.Code
}

func TestHTTP_LoginAndAuthorizationFlow(t *testing.T) {
	h := probeRouter(t)

	// Unauthenticated request → 401.
	if code := do(h, http.MethodGet, "/api/v1/read", ""); code != http.StatusUnauthorized {
		t.Errorf("no token: got %d, want 401", code)
	}

	// Bad credentials → 401.
	if code, _ := login(t, h, "admin", "wrong"); code != http.StatusUnauthorized {
		t.Errorf("bad password: got %d, want 401", code)
	}

	// Analyst: can read and write, but cannot read the audit log.
	_, analystTok := login(t, h, "ana", "anapw")
	if code := do(h, http.MethodGet, "/api/v1/read", analystTok); code != 200 {
		t.Errorf("analyst read: got %d, want 200", code)
	}
	if code := do(h, http.MethodPost, "/api/v1/write", analystTok); code != 200 {
		t.Errorf("analyst write: got %d, want 200", code)
	}
	if code := do(h, http.MethodGet, "/api/v1/audit", analystTok); code != http.StatusForbidden {
		t.Errorf("analyst audit: got %d, want 403", code)
	}

	// Admin: can read the audit log.
	_, adminTok := login(t, h, "admin", "adminpw")
	if code := do(h, http.MethodGet, "/api/v1/audit", adminTok); code != 200 {
		t.Errorf("admin audit: got %d, want 200", code)
	}

	// /auth/me echoes the principal.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+adminTok)
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("/auth/me: got %d", rec.Code)
	}
	var me struct {
		Data Principal `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatalf("decode /auth/me: %v", err)
	}
	if me.Data.Username != "admin" || me.Data.Role != RoleAdmin {
		t.Errorf("/auth/me = %+v, want admin/admin", me.Data)
	}

	// An invalid token → 401.
	if code := do(h, http.MethodGet, "/api/v1/read", "garbage.token"); code != http.StatusUnauthorized {
		t.Errorf("invalid token: got %d, want 401", code)
	}
}
