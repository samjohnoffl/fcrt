package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthEndpoints(t *testing.T) {
	cases := []struct {
		name       string
		path       string
		readiness  func(context.Context) error
		wantStatus int
		wantBody   string
	}{
		{
			name:       "liveness does not depend on the database",
			path:       "/health/live",
			readiness:  func(context.Context) error { return errors.New("db down") },
			wantStatus: http.StatusOK,
			wantBody:   `"status":"ok"`,
		},
		{
			name:       "readiness succeeds when the database responds",
			path:       "/health/ready",
			readiness:  nil, // nil = unchecked, treated as ready
			wantStatus: http.StatusOK,
			wantBody:   `"database":"unchecked"`,
		},
		{
			name:       "readiness fails when the database is unreachable",
			path:       "/health/ready",
			readiness:  func(context.Context) error { return errors.New("connection refused") },
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   `"status":"unavailable"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := NewRouter(RouterConfig{Readiness: tc.readiness})
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status=%d, want %d (body %s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.wantBody) {
				t.Fatalf("body=%s, want substring %s", rec.Body.String(), tc.wantBody)
			}
		})
	}
}
