// Package api — authz.go
//
// Authorization layer: roles, the authenticated Principal, and the
// minimum-role middleware. Authentication (authn) proves *who* the caller is;
// this file decides *what* they may do.
//
// Policy (single source of truth — keep the router in sync):
//
//	analyst  the workspace: read everything, create/lock cohorts, request and
//	         approve scenarios, run and replay experiments, create campaigns,
//	         verify the regression pack
//	admin    analyst + user management + the audit log
//
// Every authenticated route requires at least `analyst`, so a principal with a
// missing or unknown role is denied even if authentication somehow succeeded.
package api

import (
	"context"
	"log/slog"
	"net/http"
)

// Role is an authorization role. Roles are hierarchical: admin ⊃ analyst.
type Role string

const (
	RoleAnalyst Role = "analyst" // full workspace access
	RoleAdmin   Role = "admin"   // analyst + user management + audit log
)

// rank orders roles for "at least" comparisons. Unknown roles rank 0 (deny).
func (r Role) rank() int {
	switch r {
	case RoleAnalyst:
		return 1
	case RoleAdmin:
		return 2
	default:
		return 0
	}
}

// Valid reports whether r is a recognised role.
func (r Role) Valid() bool { return r.rank() > 0 }

// Principal is the authenticated caller: identity plus role.
type Principal struct {
	Username string `json:"username"`
	Role     Role   `json:"role"`
}

// AtLeast reports whether the principal's role meets the minimum required role.
func (p Principal) AtLeast(min Role) bool { return p.Role.rank() >= min.rank() }

// ctxKeyPrincipalType is a package-local context key type to avoid collisions.
type ctxKeyPrincipalType struct{}

var ctxKeyPrincipal = ctxKeyPrincipalType{}

// PrincipalFromContext returns the authenticated principal stored by
// AuthMiddleware. Returns (Principal{}, false) when the request is not
// authenticated.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(ctxKeyPrincipal).(Principal)
	if !ok || p.Username == "" {
		return Principal{}, false
	}
	return p, true
}

// RequireRole returns middleware that enforces a minimum role. It MUST be
// mounted after AuthMiddleware so a Principal is already present.
func RequireRole(min Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := PrincipalFromContext(r.Context())
			if !ok {
				writeError(w, http.StatusUnauthorized, "missing_token",
					"Authentication is required")
				return
			}
			if !p.AtLeast(min) {
				slog.Warn("authz: insufficient role",
					"user", sanitiseLog(p.Username),
					"role", string(p.Role),
					"required", string(min),
					"method", r.Method,
					"path", r.URL.Path)
				writeError(w, http.StatusForbidden, "insufficient_role",
					"Role '"+string(min)+"' or higher is required for this action")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
