// Package api — handler_user.go
//
// Admin-only user management (AUTH_MODE=local, DB-backed).
//
//	GET    /api/v1/users             list accounts
//	POST   /api/v1/users             create an account
//	PATCH  /api/v1/users/{username}  change role / reset password / disable
//	DELETE /api/v1/users/{username}  remove an account
//
// Lockout guards: an admin cannot delete, disable, or demote their own account,
// so a mistake cannot immediately lock the last administrator out.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/yourorg/financial-crime-red-team/internal/audit"
	"github.com/yourorg/financial-crime-red-team/internal/auth"
)

// Credentials authenticates a username/password pair against a user directory.
// Implemented by internal/auth.Repository (DB) and envCredentials (fallback).
type Credentials interface {
	Authenticate(ctx context.Context, username, password string) (auth.User, bool, error)
}

// UserAdmin is the management surface behind the /users endpoints.
// Implemented by internal/auth.Repository.
type UserAdmin interface {
	List(ctx context.Context) ([]auth.User, error)
	Create(ctx context.Context, username, password, role string) (auth.User, error)
	Update(ctx context.Context, username string, role, password *string, disabled *bool) (auth.User, error)
	Delete(ctx context.Context, username string) error
}

// usernamePattern keeps usernames URL- and log-safe.
var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9._-]{1,64}$`)

// UserHandler serves the /users endpoints.
type UserHandler struct {
	admin UserAdmin
	audit *audit.Repository // optional
}

// NewUserHandler creates a UserHandler. admin may be nil (management disabled).
func NewUserHandler(admin UserAdmin, auditRepo *audit.Repository) *UserHandler {
	return &UserHandler{admin: admin, audit: auditRepo}
}

type createUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

type updateUserRequest struct {
	Role     *string `json:"role,omitempty"`
	Password *string `json:"password,omitempty"`
	Disabled *bool   `json:"disabled,omitempty"`
}

// List returns every account.
func (h *UserHandler) List(w http.ResponseWriter, r *http.Request) {
	if !h.available(w) {
		return
	}
	users, err := h.admin.List(r.Context())
	if err != nil {
		slog.Error("users: list", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to list users")
		return
	}
	writeJSON(w, http.StatusOK, users)
}

// Create adds an account.
func (h *UserHandler) Create(w http.ResponseWriter, r *http.Request) {
	if !h.available(w) {
		return
	}
	var req createUserRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if msg, ok := validateNewUser(req); !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", msg)
		return
	}

	user, err := h.admin.Create(r.Context(), req.Username, req.Password, req.Role)
	switch {
	case errors.Is(err, auth.ErrUsernameTaken):
		writeError(w, http.StatusConflict, "username_taken", "That username is already taken")
		return
	case err != nil:
		slog.Error("users: create", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to create user")
		return
	}

	h.record(r, audit.EventUserCreated, user.Username, map[string]any{"role": user.Role})
	writeJSON(w, http.StatusCreated, user)
}

// Update changes a role, resets a password, or enables/disables an account.
func (h *UserHandler) Update(w http.ResponseWriter, r *http.Request) {
	if !h.available(w) {
		return
	}
	username := chi.URLParam(r, "username")
	actor, _ := PrincipalFromContext(r.Context())

	var req updateUserRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Role != nil && !auth.ValidRole(*req.Role) {
		writeError(w, http.StatusBadRequest, "invalid_role", "role must be 'analyst' or 'admin'")
		return
	}
	if req.Password != nil && len(*req.Password) < auth.MinPasswordLen {
		writeError(w, http.StatusBadRequest, "invalid_password",
			"Password must be at least "+itoa(auth.MinPasswordLen)+" characters")
		return
	}
	// Lockout guard: never let an admin lock themselves out.
	if username == actor.Username {
		if (req.Disabled != nil && *req.Disabled) || (req.Role != nil && *req.Role != string(RoleAdmin)) {
			writeError(w, http.StatusBadRequest, "self_lockout",
				"You cannot disable or demote your own account")
			return
		}
	}

	user, err := h.admin.Update(r.Context(), username, req.Role, req.Password, req.Disabled)
	switch {
	case errors.Is(err, auth.ErrUserNotFound):
		writeError(w, http.StatusNotFound, "not_found", "No such user")
		return
	case err != nil:
		slog.Error("users: update", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to update user")
		return
	}

	h.record(r, audit.EventUserUpdated, user.Username, map[string]any{
		"role": user.Role, "disabled": user.Disabled, "password_reset": req.Password != nil,
	})
	writeJSON(w, http.StatusOK, user)
}

// Delete removes an account.
func (h *UserHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if !h.available(w) {
		return
	}
	username := chi.URLParam(r, "username")
	actor, _ := PrincipalFromContext(r.Context())
	if username == actor.Username {
		writeError(w, http.StatusBadRequest, "self_lockout", "You cannot delete your own account")
		return
	}

	switch err := h.admin.Delete(r.Context(), username); {
	case errors.Is(err, auth.ErrUserNotFound):
		writeError(w, http.StatusNotFound, "not_found", "No such user")
		return
	case err != nil:
		slog.Error("users: delete", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to delete user")
		return
	}

	h.record(r, audit.EventUserDeleted, username, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "username": username})
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func (h *UserHandler) available(w http.ResponseWriter) bool {
	if h.admin == nil {
		writeError(w, http.StatusNotImplemented, "not_configured",
			"User management requires the database-backed user store (AUTH_MODE=local)")
		return false
	}
	return true
}

func validateNewUser(req createUserRequest) (string, bool) {
	if !usernamePattern.MatchString(req.Username) {
		return "Username must be 1-64 characters: letters, digits, dot, underscore or hyphen", false
	}
	if len(req.Password) < auth.MinPasswordLen {
		return "Password must be at least " + itoa(auth.MinPasswordLen) + " characters", false
	}
	if !auth.ValidRole(req.Role) {
		return "role must be 'analyst' or 'admin'", false
	}
	return "", true
}

// decodeJSON decodes a bounded JSON body, writing a 400 on failure.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Request body must be valid JSON")
		return false
	}
	return true
}

// itoa avoids importing strconv just for a small int.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func (h *UserHandler) record(r *http.Request, et audit.EventType, subject string, details map[string]any) {
	if h.audit == nil {
		return
	}
	actor, _ := PrincipalFromContext(r.Context())
	if details == nil {
		details = map[string]any{}
	}
	details["subject"] = subject
	ev := audit.Event{
		ID:         uuid.New(),
		EventType:  et,
		ActorUID:   actor.Username,
		TargetType: "user",
		TargetID:   uuid.Nil,
		Details:    details,
		IPAddress:  r.RemoteAddr,
		UserAgent:  r.UserAgent(),
		CreatedAt:  time.Now().UTC(),
	}
	if err := h.audit.Record(r.Context(), ev); err != nil {
		slog.Warn("users: record audit event", "type", string(et), "err", err)
	}
}
