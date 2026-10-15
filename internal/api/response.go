// Package api — response.go
//
// Shared HTTP response helpers.
// All API responses use these helpers to guarantee consistent JSON structure.
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// envelope wraps every success response.
type envelope struct {
	Data any `json:"data"`
}

// errorBody is the standard error response body (AC-07.1, AC-07.2).
type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// writeJSON encodes v as JSON with status code. Logs encoding failures.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(envelope{Data: v}); err != nil {
		slog.Error("response encode failed", "err", err)
	}
}

// writeError writes a structured error response (T-01, AC-07.1, AC-07.2).
// The code field is a machine-readable string; message is human-readable.
// Never include internal error details in message — use slog for those.
func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorBody{Code: code, Message: message})
}
