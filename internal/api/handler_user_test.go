// Package api — handler_user_test.go
//
// Hermetic HTTP tests for the admin-only user management endpoints. A fake
// in-memory UserAdmin stands in for the database repository.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/yourorg/financial-crime-red-team/internal/auth"
)

// ─── Fake directory ───────────────────────────────────────────────────────────

type fakeUsers struct {
	users map[string]auth.User
}

func newFakeUsers() *fakeUsers {
	return &fakeUsers{users: map[string]auth.User{
		"admin": {Username: "admin", Role: auth.RoleAdmin},
		"ana":   {Username: "ana", Role: auth.RoleAnalyst},
	}}
}

func (f *fakeUsers) List(context.Context) ([]auth.User, error) {
	out := make([]auth.User, 0, len(f.users))
	for _, u := range f.users {
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Username < out[j].Username })
	return out, nil
}

func (f *fakeUsers) Create(_ context.Context, username, password, role string) (auth.User, error) {
	if _, ok := f.users[username]; ok {
		return auth.User{}, auth.ErrUsernameTaken
	}
	if len(password) < auth.MinPasswordLen {
		return auth.User{}, errors.New("password too short")
	}
	if !auth.ValidRole(role) {
		return auth.User{}, auth.ErrInvalidRole
	}
	u := auth.User{Username: username, Role: role}
	f.users[username] = u
	return u, nil
}

func (f *fakeUsers) Update(_ context.Context, username string, role, _ *string, disabled *bool) (auth.User, error) {
	u, ok := f.users[username]
	if !ok {
		return auth.User{}, auth.ErrUserNotFound
	}
	if role != nil {
		if !auth.ValidRole(*role) {
			return auth.User{}, auth.ErrInvalidRole
		}
		u.Role = *role
	}
	if disabled != nil {
		u.Disabled = *disabled
	}
	f.users[username] = u
	return u, nil
}

func (f *fakeUsers) Delete(_ context.Context, username string) error {
	if _, ok := f.users[username]; !ok {
		return auth.ErrUserNotFound
	}
	delete(f.users, username)
	return nil
}

// ─── Test harness ─────────────────────────────────────────────────────────────

func userRouter(h *UserHandler, actor Principal, requireAdmin bool) http.Handler {
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				ctx := context.WithValue(req.Context(), ctxKeyPrincipal, actor)
				next.ServeHTTP(w, req.WithContext(ctx))
			})
		})
		if requireAdmin {
			r.Use(RequireRole(RoleAdmin))
		}
		r.Get("/users", h.List)
		r.Post("/users", h.Create)
		r.Patch("/users/{username}", h.Update)
		r.Delete("/users/{username}", h.Delete)
	})
	return r
}

func call(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	h.ServeHTTP(rec, req)
	return rec
}

func adminActor() Principal { return Principal{Username: "admin", Role: RoleAdmin} }

// ─── Tests ────────────────────────────────────────────────────────────────────

func TestUserHandler_NotConfiguredReturns501(t *testing.T) {
	h := NewUserHandler(nil, nil)
	rec := call(userRouter(h, adminActor(), true), http.MethodGet, "/api/v1/users", "")
	if rec.Code != http.StatusNotImplemented {
		t.Errorf("got %d, want 501", rec.Code)
	}
}

func TestUserHandler_List(t *testing.T) {
	h := NewUserHandler(newFakeUsers(), nil)
	rec := call(userRouter(h, adminActor(), true), http.MethodGet, "/api/v1/users", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
	var env struct {
		Data []auth.User `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(env.Data) != 2 || env.Data[0].Username != "admin" {
		t.Errorf("unexpected list: %+v", env.Data)
	}
}

func TestUserHandler_CreateAndValidation(t *testing.T) {
	h := NewUserHandler(newFakeUsers(), nil)
	r := userRouter(h, adminActor(), true)

	// Valid creation.
	rec := call(r, http.MethodPost, "/api/v1/users",
		`{"username":"new.analyst","password":"secret123","role":"analyst"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: got %d, want 201 (%s)", rec.Code, rec.Body.String())
	}

	// Duplicate → 409.
	rec = call(r, http.MethodPost, "/api/v1/users",
		`{"username":"new.analyst","password":"secret123","role":"analyst"}`)
	if rec.Code != http.StatusConflict {
		t.Errorf("duplicate: got %d, want 409", rec.Code)
	}

	// Invalid role → 400.
	rec = call(r, http.MethodPost, "/api/v1/users",
		`{"username":"x","password":"secret123","role":"viewer"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid role: got %d, want 400", rec.Code)
	}

	// Short password → 400.
	rec = call(r, http.MethodPost, "/api/v1/users",
		`{"username":"y","password":"123","role":"analyst"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("short password: got %d, want 400", rec.Code)
	}

	// Bad username characters → 400.
	rec = call(r, http.MethodPost, "/api/v1/users",
		`{"username":"bad name!","password":"secret123","role":"analyst"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad username: got %d, want 400", rec.Code)
	}

	// Malformed JSON → 400.
	rec = call(r, http.MethodPost, "/api/v1/users", `{not json`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("malformed body: got %d, want 400", rec.Code)
	}
}

func TestUserHandler_Update(t *testing.T) {
	h := NewUserHandler(newFakeUsers(), nil)
	r := userRouter(h, adminActor(), true)

	// Promote an analyst.
	rec := call(r, http.MethodPatch, "/api/v1/users/ana", `{"role":"admin"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: got %d, want 200 (%s)", rec.Code, rec.Body.String())
	}

	// Unknown user → 404.
	rec = call(r, http.MethodPatch, "/api/v1/users/ghost", `{"role":"analyst"}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown user: got %d, want 404", rec.Code)
	}

	// Invalid role → 400.
	rec = call(r, http.MethodPatch, "/api/v1/users/ana", `{"role":"viewer"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid role: got %d, want 400", rec.Code)
	}

	// Self-lockout guard: cannot disable or demote yourself.
	rec = call(r, http.MethodPatch, "/api/v1/users/admin", `{"disabled":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("self-disable: got %d, want 400", rec.Code)
	}
	rec = call(r, http.MethodPatch, "/api/v1/users/admin", `{"role":"analyst"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("self-demote: got %d, want 400", rec.Code)
	}
	// ...but changing your own password is fine.
	rec = call(r, http.MethodPatch, "/api/v1/users/admin", `{"password":"newsecret"}`)
	if rec.Code != http.StatusOK {
		t.Errorf("self password change: got %d, want 200", rec.Code)
	}
}

func TestUserHandler_Delete(t *testing.T) {
	h := NewUserHandler(newFakeUsers(), nil)
	r := userRouter(h, adminActor(), true)

	if rec := call(r, http.MethodDelete, "/api/v1/users/ana", ""); rec.Code != http.StatusOK {
		t.Fatalf("delete: got %d, want 200", rec.Code)
	}
	if rec := call(r, http.MethodDelete, "/api/v1/users/ana", ""); rec.Code != http.StatusNotFound {
		t.Errorf("delete twice: got %d, want 404", rec.Code)
	}
	// Self-lockout guard.
	if rec := call(r, http.MethodDelete, "/api/v1/users/admin", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("self-delete: got %d, want 400", rec.Code)
	}
}

func TestUserHandler_AnalystForbidden(t *testing.T) {
	h := NewUserHandler(newFakeUsers(), nil)
	analyst := Principal{Username: "ana", Role: RoleAnalyst}
	r := userRouter(h, analyst, true)

	if rec := call(r, http.MethodGet, "/api/v1/users", ""); rec.Code != http.StatusForbidden {
		t.Errorf("analyst list: got %d, want 403", rec.Code)
	}
	if rec := call(r, http.MethodPost, "/api/v1/users", `{"username":"z","password":"secret123","role":"analyst"}`); rec.Code != http.StatusForbidden {
		t.Errorf("analyst create: got %d, want 403", rec.Code)
	}
}
