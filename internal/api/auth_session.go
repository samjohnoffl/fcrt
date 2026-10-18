// Package api — auth_session.go
//
// Username/password authentication with stateless HMAC-signed session tokens.
//
// Flow:
//
//	POST /api/v1/auth/login  {username,password} -> {token, expires_at, user}
//	...subsequent requests:  Authorization: Bearer <token>
//	GET  /api/v1/auth/me     -> current principal
//	POST /api/v1/auth/logout -> no-op (client discards the token)
//
// Users come from the AUTH_USERS env var (username:password:role). This is
// intentionally dependency-free and stateless: no user table, no sessions
// store, nothing to migrate. Passwords are compared in constant time.
//
// ⚠️  Passwords are held in memory as plaintext from configuration. That is
// acceptable for test/dev credentials; do not use it for real user accounts.
package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/yourorg/financial-crime-red-team/internal/audit"
	"github.com/yourorg/financial-crime-red-team/internal/auth"
)

// ─── User store ───────────────────────────────────────────────────────────────

// User is a locally-configured username/password account.
type User struct {
	Username string
	Password string
	Role     Role
}

// UserStore holds the configured users keyed by username.
type UserStore struct {
	users map[string]User
}

// DefaultUserSpec is used when AUTH_USERS is empty. It is deliberately obvious
// so it is easy to spot in logs and change.
const DefaultUserSpec = "admin:admin:admin,analyst:analyst:analyst"

// NewUserStore parses an AUTH_USERS spec of the form
// "username:password:role[,...]". An empty spec falls back to DefaultUserSpec.
// Passwords must not contain ':' or ','.
func NewUserStore(spec string) (*UserStore, error) {
	if strings.TrimSpace(spec) == "" {
		spec = DefaultUserSpec
	}
	store := &UserStore{users: make(map[string]User)}
	for _, entry := range strings.Split(spec, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.Split(entry, ":")
		if len(parts) != 3 {
			return nil, fmt.Errorf("auth: AUTH_USERS entry %q must be username:password:role", entry)
		}
		u := User{Username: parts[0], Password: parts[1], Role: Role(parts[2])}
		if u.Username == "" || u.Password == "" {
			return nil, fmt.Errorf("auth: AUTH_USERS entry %q has an empty username or password", entry)
		}
		if !u.Role.Valid() {
			return nil, fmt.Errorf("auth: AUTH_USERS entry %q has invalid role %q (want analyst|admin)", entry, u.Role)
		}
		store.users[u.Username] = u
	}
	if len(store.users) == 0 {
		return nil, fmt.Errorf("auth: no users configured")
	}
	return store, nil
}

// Authenticate verifies a username/password pair in constant time.
func (s *UserStore) Authenticate(username, password string) (User, bool) {
	u, ok := s.users[username]
	if !ok {
		// Burn a comparison so a missing user is not faster than a wrong password.
		subtle.ConstantTimeCompare([]byte(password), []byte("no-such-user-timing-pad"))
		return User{}, false
	}
	if subtle.ConstantTimeCompare([]byte(u.Password), []byte(password)) != 1 {
		return User{}, false
	}
	return u, true
}

// All returns every configured user. Used to seed the database directory.
func (s *UserStore) All() []User {
	out := make([]User, 0, len(s.users))
	for _, u := range s.users {
		out = append(out, u)
	}
	return out
}

// UsesDefaultCredentials reports whether any account still has its password
// equal to its username (e.g. admin/admin). Used to emit a startup warning.
func (s *UserStore) UsesDefaultCredentials() bool {
	for _, u := range s.users {
		if u.Username == u.Password || u.Password == "admin" || u.Password == "password" {
			return true
		}
	}
	return false
}

// ─── Session tokens ───────────────────────────────────────────────────────────

// SessionSigner issues and verifies HMAC-SHA256 signed session tokens.
// Token format: base64url(payloadJSON) + "." + base64url(hmac).
type SessionSigner struct {
	secret []byte
	ttl    time.Duration
}

// NewSessionSigner builds a signer. ttl <= 0 defaults to 12h.
func NewSessionSigner(secret string, ttl time.Duration) *SessionSigner {
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	return &SessionSigner{secret: []byte(secret), ttl: ttl}
}

// TTL returns the configured token lifetime.
func (s *SessionSigner) TTL() time.Duration { return s.ttl }

type sessionClaims struct {
	Sub  string `json:"sub"`
	Role Role   `json:"role"`
	Iat  int64  `json:"iat"`
	Exp  int64  `json:"exp"`
}

// Sign issues a session token for the principal and returns it with its expiry.
func (s *SessionSigner) Sign(p Principal, now time.Time) (string, time.Time) {
	exp := now.Add(s.ttl)
	claims := sessionClaims{Sub: p.Username, Role: p.Role, Iat: now.Unix(), Exp: exp.Unix()}
	payload, _ := json.Marshal(claims) // sessionClaims is always marshallable
	body := base64.RawURLEncoding.EncodeToString(payload)
	return body + "." + s.signature(body), exp
}

// Verify validates a session token and returns the principal it encodes.
// The error message contains "expired" when the token has expired so the
// middleware can map it to the token_expired code (AC-07.2).
func (s *SessionSigner) Verify(token string, now time.Time) (Principal, error) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return Principal{}, fmt.Errorf("auth: malformed session token")
	}
	if subtle.ConstantTimeCompare([]byte(s.signature(parts[0])), []byte(parts[1])) != 1 {
		return Principal{}, fmt.Errorf("auth: invalid session signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Principal{}, fmt.Errorf("auth: decode session token: %w", err)
	}
	var claims sessionClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return Principal{}, fmt.Errorf("auth: parse session token: %w", err)
	}
	if claims.Exp <= now.Unix() {
		return Principal{}, fmt.Errorf("auth: session token expired")
	}
	if claims.Sub == "" || !claims.Role.Valid() {
		return Principal{}, fmt.Errorf("auth: invalid session token claims")
	}
	return Principal{Username: claims.Sub, Role: claims.Role}, nil
}

func (s *SessionSigner) signature(body string) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(body))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// SessionVerifier adapts a SessionSigner to the AuthVerifier interface.
type SessionVerifier struct {
	signer *SessionSigner
}

// NewSessionVerifier creates a SessionVerifier.
func NewSessionVerifier(signer *SessionSigner) *SessionVerifier {
	return &SessionVerifier{signer: signer}
}

// Verify implements AuthVerifier.
func (v *SessionVerifier) Verify(_ context.Context, token string) (Principal, error) {
	return v.signer.Verify(token, time.Now())
}

// ─── Credentials adapters ─────────────────────────────────────────────────────

// envCredentials adapts the environment-configured UserStore to the Credentials
// interface. It is the fallback directory when the database-backed store is not
// available (AUTH_MODE=local without a database, or tests).
type envCredentials struct{ store *UserStore }

// NewEnvCredentials wraps a UserStore as a Credentials source.
func NewEnvCredentials(store *UserStore) Credentials { return envCredentials{store: store} }

// Authenticate implements Credentials.
func (e envCredentials) Authenticate(_ context.Context, username, password string) (auth.User, bool, error) {
	u, ok := e.store.Authenticate(username, password)
	if !ok {
		return auth.User{}, false, nil
	}
	return auth.User{Username: u.Username, Role: string(u.Role)}, true, nil
}

// ─── HTTP handlers ────────────────────────────────────────────────────────────

// AuthHandler serves the /auth endpoints.
type AuthHandler struct {
	creds  Credentials
	signer *SessionSigner
	audit  *audit.Repository // optional
}

// NewAuthHandler creates an AuthHandler.
func NewAuthHandler(creds Credentials, signer *SessionSigner, auditRepo *audit.Repository) *AuthHandler {
	return &AuthHandler{creds: creds, signer: signer, audit: auditRepo}
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token     string    `json:"token"`
	TokenType string    `json:"token_type"`
	ExpiresAt time.Time `json:"expires_at"`
	User      Principal `json:"user"`
}

// Login authenticates a username/password pair and returns a session token.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	if h.creds == nil {
		writeError(w, http.StatusNotImplemented, "not_configured",
			"Username/password login is not enabled on this deployment")
		return
	}
	var req loginRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Request body must be JSON {username, password}")
		return
	}

	user, ok, err := h.creds.Authenticate(r.Context(), req.Username, req.Password)
	if err != nil {
		slog.Error("auth: credential lookup failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error",
			"Authentication is temporarily unavailable")
		return
	}
	if !ok {
		slog.Warn("auth: login failed",
			"username", sanitiseLog(req.Username),
			"ip", r.RemoteAddr)
		h.recordEvent(r, audit.EventLoginFailed, req.Username, map[string]any{"ip": r.RemoteAddr})
		writeError(w, http.StatusUnauthorized, "invalid_credentials",
			"Invalid username or password")
		return
	}
	if !Role(user.Role).Valid() {
		slog.Warn("auth: user has an invalid role", "username", sanitiseLog(user.Username), "role", user.Role)
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "Invalid username or password")
		return
	}

	principal := Principal{Username: user.Username, Role: Role(user.Role)}
	token, exp := h.signer.Sign(principal, time.Now())
	slog.Info("auth: login succeeded", "username", user.Username, "role", string(user.Role))
	h.recordEvent(r, audit.EventLoginSucceeded, user.Username, map[string]any{"role": string(user.Role)})

	writeJSON(w, http.StatusOK, loginResponse{
		Token:     token,
		TokenType: "Bearer",
		ExpiresAt: exp,
		User:      principal,
	})
}

// Me returns the authenticated principal.
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	p, ok := PrincipalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing_token", "Authentication is required")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// Logout is a no-op for stateless tokens; the client discards the token.
// It exists so the frontend has a single place to call.
func (h *AuthHandler) Logout(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func (h *AuthHandler) recordEvent(r *http.Request, et audit.EventType, actor string, details map[string]any) {
	if h.audit == nil {
		return
	}
	ev := audit.Event{
		ID:         uuid.New(),
		EventType:  et,
		ActorUID:   actor,
		TargetType: "auth",
		TargetID:   uuid.Nil,
		Details:    details,
		IPAddress:  r.RemoteAddr,
		UserAgent:  r.UserAgent(),
		CreatedAt:  time.Now().UTC(),
	}
	if err := h.audit.Record(r.Context(), ev); err != nil {
		slog.Warn("auth: record audit event", "type", string(et), "err", err)
	}
}
