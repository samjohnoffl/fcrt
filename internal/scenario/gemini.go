// Package scenario — gemini.go
//
// GeminiProvider is the backend-only interface to the Gemini model via Vertex AI.
//
// Security model:
//   - All model output is treated as untrusted data (AGENTS.md).
//   - Generated text is never executed, eval'd, or interpolated into SQL.
//   - Every request is tagged with a CorrelationID for auditability.
//
// Reliability model:
//   - Per-attempt context timeout (default 30 s).
//   - Bounded exponential-backoff retries (max 3, configurable up to 5).
//   - Cost safeguards: MaxOutputTokens cap, per-request prompt-token budget.
//   - Response body capped at 1 MiB to prevent memory exhaustion.
//   - Provider failures surface as GeminiProviderError, not panics.
package scenario

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// ─── Interface ────────────────────────────────────────────────────────────────

// GeminiProvider is the backend-only interface for scenario generation.
// Callers hold this interface, not the concrete type, enabling test mocks.
//
// Contract:
//   - Raw model output must not escape this call unvalidated.
//   - Every call must log req.CorrelationID (AGENTS.md idempotency rule).
//   - ctx cancellation / deadline must be respected.
type GeminiProvider interface {
	Generate(ctx context.Context, req GenerationRequest) (GenerationResult, error)
}

// ─── Errors ───────────────────────────────────────────────────────────────────

// GeminiProviderError wraps an upstream error from the Vertex AI API.
type GeminiProviderError struct {
	StatusCode    int
	CorrelationID uuid.UUID
	Message       string
}

func (e *GeminiProviderError) Error() string {
	return fmt.Sprintf("gemini provider error [correlation=%s status=%d]: %s",
		e.CorrelationID, e.StatusCode, e.Message)
}

var (
	// ErrProviderUnavailable is returned when the upstream API is unreachable after all retries.
	ErrProviderUnavailable = errors.New("gemini provider unavailable after retries")

	// ErrBudgetExceeded is returned when the per-request cost safeguard trips.
	ErrBudgetExceeded = errors.New("gemini request budget exceeded")

	// ErrNoCandidates is returned when the model response contains no usable candidates.
	ErrNoCandidates = errors.New("gemini returned no valid candidates")
)

// ─── Client configuration ─────────────────────────────────────────────────────

// ClientConfig holds all tunable parameters for the Vertex AI client.
// Construct once via NewVertexClient; treat as immutable after that.
type ClientConfig struct {
	// ProjectID is the GCP project hosting the Vertex AI endpoint.
	ProjectID string

	// Location is the Vertex AI region, e.g. "us-central1".
	Location string

	// ModelID is the Gemini model version, e.g. "gemini-1.5-pro".
	ModelID string

	// MaxOutputTokens caps the model response size for cost control.
	// Default: 2048. Allowed range: [256, 8192].
	MaxOutputTokens int

	// Temperature controls generation randomness. Default: 0.4.
	// Range [0.0, 1.0]. Lower = more deterministic output.
	Temperature float64

	// RequestTimeout is the per-attempt deadline. Default: 30 s.
	RequestTimeout time.Duration

	// MaxRetries is the maximum number of retry attempts on 5xx errors.
	// Default: 3. Maximum: 5.
	MaxRetries int

	// RetryBaseDelay is the initial backoff interval. Default: 500 ms.
	RetryBaseDelay time.Duration

	// MaxPromptTokenBudget is the maximum estimated prompt-token count allowed
	// per generation request (cost safeguard). Default: 4096.
	MaxPromptTokenBudget int
}

// DefaultClientConfig returns sensible production defaults.
func DefaultClientConfig(projectID, location string) ClientConfig {
	return ClientConfig{
		ProjectID:            projectID,
		Location:             location,
		ModelID:              "gemini-1.5-pro",
		MaxOutputTokens:      2048,
		Temperature:          0.4,
		RequestTimeout:       30 * time.Second,
		MaxRetries:           3,
		RetryBaseDelay:       500 * time.Millisecond,
		MaxPromptTokenBudget: 4096,
	}
}

func (c *ClientConfig) validate() error {
	if c.ProjectID == "" {
		return fmt.Errorf("ClientConfig: ProjectID is required")
	}
	if c.Location == "" {
		return fmt.Errorf("ClientConfig: Location is required")
	}
	if c.MaxOutputTokens < 256 || c.MaxOutputTokens > 8192 {
		return fmt.Errorf("ClientConfig: MaxOutputTokens must be in [256, 8192]")
	}
	if c.Temperature < 0 || c.Temperature > 1 {
		return fmt.Errorf("ClientConfig: Temperature must be in [0.0, 1.0]")
	}
	if c.MaxRetries < 0 || c.MaxRetries > 5 {
		return fmt.Errorf("ClientConfig: MaxRetries must be in [0, 5]")
	}
	return nil
}

// ─── Vertex AI HTTP client ────────────────────────────────────────────────────

// vertexGeminiProvider implements GeminiProvider via the Vertex AI REST API.
// It uses Application Default Credentials (ADC) for authentication.
type vertexGeminiProvider struct {
	cfg      ClientConfig
	hc       *http.Client
	endpoint string
}

// NewVertexClient constructs a GeminiProvider backed by the Vertex AI REST API.
// Authentication uses ADC — set GOOGLE_APPLICATION_CREDENTIALS or run on GCE/Cloud Run.
func NewVertexClient(cfg ClientConfig) (GeminiProvider, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	// Obtain an ADC token source for the Vertex AI OAuth scope.
	ts, err := google.DefaultTokenSource(
		context.Background(),
		"https://www.googleapis.com/auth/cloud-platform",
	)
	if err != nil {
		return nil, fmt.Errorf("gemini: ADC token source: %w", err)
	}

	// oauth2.Transport injects a Bearer token on every request and handles
	// automatic refresh. This is the approved oauth2 pattern for Go.
	hc := &http.Client{
		Transport: &oauth2.Transport{
			Source: ts,
			Base:   http.DefaultTransport,
		},
	}

	endpoint := fmt.Sprintf(
		"https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s"+
			"/publishers/google/models/%s:generateContent",
		cfg.Location, cfg.ProjectID, cfg.Location, cfg.ModelID,
	)

	return &vertexGeminiProvider{cfg: cfg, hc: hc, endpoint: endpoint}, nil
}

// Generate calls Vertex AI, validates the response via ParseAndValidate, and
// returns a GenerationResult. 5xx errors trigger bounded exponential-backoff
// retries; all other errors are returned immediately (non-retryable).
func (v *vertexGeminiProvider) Generate(ctx context.Context, req GenerationRequest) (GenerationResult, error) {
	if req.MaxCandidates < 1 || req.MaxCandidates > 10 {
		return GenerationResult{}, fmt.Errorf("gemini: MaxCandidates must be in [1, 10]")
	}

	prompt := buildPrompt(req)

	// Cost safeguard: rough estimate (4 chars ≈ 1 token).
	if len(prompt)/4 > v.cfg.MaxPromptTokenBudget {
		return GenerationResult{}, ErrBudgetExceeded
	}

	var lastErr error
	for attempt := 0; attempt <= v.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(float64(v.cfg.RetryBaseDelay) * math.Pow(2, float64(attempt-1)))
			select {
			case <-ctx.Done():
				return GenerationResult{}, ctx.Err()
			case <-time.After(delay):
			}
		}

		res, err := v.doRequest(ctx, req.CorrelationID, prompt)
		if err == nil {
			return res, nil
		}

		// Retry only on transient server errors.
		var provErr *GeminiProviderError
		if errors.As(err, &provErr) && provErr.StatusCode >= 500 {
			lastErr = err
			continue
		}
		return GenerationResult{}, err // non-retryable
	}

	return GenerationResult{}, fmt.Errorf("%w: %v", ErrProviderUnavailable, lastErr)
}

// doRequest executes one HTTP attempt against the Vertex AI generateContent endpoint.
func (v *vertexGeminiProvider) doRequest(
	ctx context.Context,
	correlationID uuid.UUID,
	prompt string,
) (GenerationResult, error) {
	reqCtx, cancel := context.WithTimeout(ctx, v.cfg.RequestTimeout)
	defer cancel()

	body := buildVertexRequest(prompt, v.cfg.MaxOutputTokens, v.cfg.Temperature)
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return GenerationResult{}, fmt.Errorf("gemini: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, v.endpoint, bytes.NewReader(bodyJSON))
	if err != nil {
		return GenerationResult{}, fmt.Errorf("gemini: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Correlation-ID", correlationID.String())

	resp, err := v.hc.Do(httpReq)
	if err != nil {
		return GenerationResult{}, &GeminiProviderError{
			StatusCode: 0, CorrelationID: correlationID,
			Message: sanitise(err.Error()),
		}
	}
	defer resp.Body.Close()

	// Cap response body at 1 MiB to prevent memory exhaustion.
	rawBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return GenerationResult{}, fmt.Errorf("gemini: read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return GenerationResult{}, &GeminiProviderError{
			StatusCode:    resp.StatusCode,
			CorrelationID: correlationID,
			Message:       "non-200: " + truncate(string(rawBody), 256),
		}
	}

	// Extract the model text from the Vertex AI envelope.
	candidatesJSON, promptTokens, outputTokens, err := extractCandidatesJSON(rawBody)
	if err != nil {
		return GenerationResult{}, fmt.Errorf("gemini: extract candidates: %w", err)
	}

	// Security boundary: all model text is validated before use.
	accepted, rejected := ParseAndValidate(candidatesJSON, correlationID)

	return GenerationResult{
		Accepted:      accepted,
		Rejected:      rejected,
		CorrelationID: correlationID,
		PromptTokens:  promptTokens,
		OutputTokens:  outputTokens,
	}, nil
}

// ─── Vertex AI request / response types ──────────────────────────────────────

type vertexRequest struct {
	Contents         []vertexContent `json:"contents"`
	GenerationConfig *genConfig      `json:"generationConfig,omitempty"`
}

type vertexContent struct {
	Role  string       `json:"role"`
	Parts []vertexPart `json:"parts"`
}

type vertexPart struct {
	Text string `json:"text"`
}

type genConfig struct {
	Temperature      float64 `json:"temperature"`
	MaxOutputTokens  int     `json:"maxOutputTokens"`
	// ResponseMimeType requests structured JSON output from the model.
	ResponseMimeType string  `json:"responseMimeType,omitempty"`
}

func buildVertexRequest(prompt string, maxTokens int, temp float64) vertexRequest {
	return vertexRequest{
		Contents: []vertexContent{{
			Role:  "user",
			Parts: []vertexPart{{Text: prompt}},
		}},
		GenerationConfig: &genConfig{
			Temperature:      temp,
			MaxOutputTokens:  maxTokens,
			ResponseMimeType: "application/json",
		},
	}
}

// vertexResponse mirrors the Vertex AI generateContent response envelope.
// Only fields needed for candidate extraction and usage tracking are declared.
type vertexResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
	} `json:"usageMetadata"`
}

// extractCandidatesJSON extracts the model-generated text from the Vertex AI
// response envelope and returns it as raw bytes for ParseAndValidate.
func extractCandidatesJSON(rawBody []byte) ([]byte, int, int, error) {
	var vr vertexResponse
	if err := json.Unmarshal(rawBody, &vr); err != nil {
		return nil, 0, 0, fmt.Errorf("parse vertex response: %w", err)
	}
	if len(vr.Candidates) == 0 {
		return nil, 0, 0, ErrNoCandidates
	}
	var sb strings.Builder
	for _, p := range vr.Candidates[0].Content.Parts {
		sb.WriteString(p.Text)
	}
	text := strings.TrimSpace(sb.String())
	if text == "" {
		return nil, 0, 0, ErrNoCandidates
	}
	return []byte(text),
		vr.UsageMetadata.PromptTokenCount,
		vr.UsageMetadata.CandidatesTokenCount,
		nil
}

// ─── Prompt builder ───────────────────────────────────────────────────────────

// buildPrompt constructs the Vertex AI prompt for a GenerationRequest.
// The prompt embeds the approved JSON schema so the model knows exactly what
// fields to produce. No row-level cohort data is included; only statistics.
func buildPrompt(req GenerationRequest) string {
	schema := `{
  "candidates": [
    {
      "mutation_type": "<one of: split_transaction | delay_and_resubmit | introduce_shell_entity>",
      "declared_change": "<human description, max 1024 chars>",
      "mutation_spec": {
        "split_count": <int 2-20, required for split_transaction>,
        "threshold_cents": <int64 >0, required for split_transaction and introduce_shell_entity>,
        "delay_minutes": <int 1-1440, required for delay_and_resubmit>
      },
      "rationale": "<why this tests AML evasion, max 4096 chars>",
      "tags": ["<short label>"],
      "eiv_confidence": <float 0.0-1.0>
    }
  ]
}`

	cs := req.CohortSummary
	return fmt.Sprintf(
		`You are a financial crime red-team assistant. Propose synthetic AML evasion scenarios for testing only. No real customer data is involved.

Cohort statistics:
- Entities: %d, Accounts: %d, Transactions: %d over %d days
- Top countries: %s
- Current alert rate: %.1f%%

Generate exactly %d distinct scenario candidates. Use ONLY the JSON schema below. Do NOT add fields not shown. Do NOT include SQL, code, shell commands, or executable content.

%s

Output ONLY valid JSON. No markdown fences, no comments, no text outside the JSON object.`,
		cs.EntityCount, cs.AccountCount, cs.TxCount, cs.WindowDays,
		strings.Join(cs.TopCountryCodes, ", "),
		cs.AlertRatePercent,
		req.MaxCandidates,
		schema,
	)
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
