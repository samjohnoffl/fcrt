// Gemini API (API-key) provider tests — hermetic; a local httptest server
// stands in for generativelanguage.googleapis.com. No network, no real key.

package scenario

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// geminiAPIEnvelope wraps model output text in the Gemini API response shape.
func geminiAPIEnvelope(modelText string) string {
	b, _ := json.Marshal(map[string]any{
		"candidates": []any{map[string]any{
			"content":      map[string]any{"parts": []any{map[string]any{"text": modelText}}},
			"finishReason": "STOP",
		}},
		"usageMetadata": map[string]any{"promptTokenCount": 12, "candidatesTokenCount": 34},
	})
	return string(b)
}

func apiTestConfig() ClientConfig {
	cfg := DefaultClientConfig("", "")
	cfg.MaxRetries = 2
	cfg.RetryBaseDelay = time.Millisecond
	return cfg
}

func genReq() GenerationRequest {
	return GenerationRequest{
		CorrelationID: uuid.New(),
		MaxCandidates: 2,
		CohortSummary: CohortSummary{
			EntityCount: 100, AccountCount: 200, TxCount: 500,
			WindowDays: 90, TopCountryCodes: []string{"US", "GB"}, AlertRatePercent: 3.5,
		},
	}
}

func TestNewGeminiAPIClient_RequiresAPIKey(t *testing.T) {
	if _, err := NewGeminiAPIClient("   ", apiTestConfig()); err == nil {
		t.Fatal("expected an error for an empty API key")
	}
}

func TestNewGeminiAPIClient_DefaultsModel(t *testing.T) {
	cfg := apiTestConfig()
	cfg.ModelID = "" // let the constructor pick the default
	p, err := NewGeminiAPIClient("test-key", cfg)
	if err != nil {
		t.Fatalf("NewGeminiAPIClient: %v", err)
	}
	pp, ok := p.(*geminiHTTPProvider)
	if !ok {
		t.Fatalf("expected *geminiHTTPProvider, got %T", p)
	}
	if !strings.Contains(pp.endpoint, DefaultModel) {
		t.Errorf("endpoint %q should contain model %q", pp.endpoint, DefaultModel)
	}
	if !strings.Contains(pp.endpoint, "generativelanguage.googleapis.com") {
		t.Errorf("endpoint should target the Gemini API: %q", pp.endpoint)
	}
	if pp.headers["x-goog-api-key"] != "test-key" {
		t.Errorf("API key must be sent in the x-goog-api-key header, got %v", pp.headers)
	}
	if !pp.stripSchemaKeys {
		t.Error("Gemini API provider must strip unsupported schema keys")
	}
}

func TestGeminiAPIProvider_ParsesCandidatesAndSanitizesSchema(t *testing.T) {
	var gotBody []byte
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-goog-api-key")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, geminiAPIEnvelope(validCandidateJSON("")))
	}))
	defer srv.Close()

	p := newGeminiHTTPProvider("secret-key", apiTestConfig(), srv.URL, srv.Client())
	p.stripSchemaKeys = true

	res, err := p.Generate(context.Background(), genReq())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(res.Accepted) != 1 {
		t.Fatalf("expected 1 accepted candidate, got %d (rejected=%d)", len(res.Accepted), len(res.Rejected))
	}
	if res.PromptTokens != 12 || res.OutputTokens != 34 {
		t.Errorf("usage tokens = (%d,%d), want (12,34)", res.PromptTokens, res.OutputTokens)
	}
	if res.ModelID == "" {
		t.Error("result must record the model that produced it")
	}
	if gotKey != "secret-key" {
		t.Errorf("server saw x-goog-api-key=%q, want secret-key", gotKey)
	}
	// The Gemini API rejects additionalProperties, so it must be stripped.
	if strings.Contains(string(gotBody), "additionalProperties") {
		t.Error("request body must not contain additionalProperties")
	}
	// The response schema must still be present and well-formed.
	var body struct {
		GenerationConfig struct {
			ResponseMIMEType string         `json:"responseMimeType"`
			ResponseSchema   map[string]any `json:"responseSchema"`
		} `json:"generationConfig"`
	}
	if err := json.Unmarshal(gotBody, &body); err != nil {
		t.Fatalf("request body is not valid JSON: %v", err)
	}
	if body.GenerationConfig.ResponseMIMEType != "application/json" {
		t.Errorf("responseMimeType = %q", body.GenerationConfig.ResponseMIMEType)
	}
	if body.GenerationConfig.ResponseSchema == nil {
		t.Error("responseSchema must be present")
	}
}

func TestGeminiAPIProvider_RetriesOn5xx(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(w, `{"error":{"message":"high demand"}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, geminiAPIEnvelope(validCandidateJSON("")))
	}))
	defer srv.Close()

	p := newGeminiHTTPProvider("k", apiTestConfig(), srv.URL, srv.Client())
	res, err := p.Generate(context.Background(), genReq())
	if err != nil {
		t.Fatalf("Generate should succeed after a retry: %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 calls (1 retry), got %d", calls)
	}
	if len(res.Accepted) != 1 {
		t.Errorf("expected 1 accepted candidate, got %d", len(res.Accepted))
	}
}

func TestGeminiAPIProvider_DoesNotRetryOn4xx(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":{"message":"bad request"}}`)
	}))
	defer srv.Close()

	p := newGeminiHTTPProvider("k", apiTestConfig(), srv.URL, srv.Client())
	if _, err := p.Generate(context.Background(), genReq()); err == nil {
		t.Fatal("expected an error on 400")
	}
	if calls != 1 {
		t.Errorf("4xx must not be retried; got %d calls", calls)
	}
}

func TestGeminiAPIProvider_NoCandidates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"candidates":[],"usageMetadata":{}}`)
	}))
	defer srv.Close()

	p := newGeminiHTTPProvider("k", apiTestConfig(), srv.URL, srv.Client())
	if _, err := p.Generate(context.Background(), genReq()); err == nil {
		t.Fatal("expected an error when the response has no candidates")
	}
}

func TestStripUnsupportedSchemaKeys(t *testing.T) {
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"items": map[string]any{
				"type":                 "array",
				"additionalProperties": false,
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"enum":                 []any{"a", "b"},
				},
			},
		},
	}
	stripUnsupportedSchemaKeys(schema)

	encoded, _ := json.Marshal(schema)
	if strings.Contains(string(encoded), "additionalProperties") {
		t.Errorf("additionalProperties must be removed at every level: %s", encoded)
	}
	if !strings.Contains(string(encoded), "\"enum\":[\"a\",\"b\"]") {
		t.Errorf("enums must be preserved: %s", encoded)
	}
}
