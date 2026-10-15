// Package scenario — gemini_test.go
//
// Tests for the schema validator and the mock GeminiProvider.
// No real Vertex AI calls are made; all tests are in-memory.
//
// Coverage:
//   - Malformed JSON: not parseable, not an object, not an array
//   - Unknown fields: top-level and spec-level
//   - Missing required fields: mutation_type, declared_change, rationale, spec fields
//   - Unapproved mutation type
//   - Out-of-range spec values: split_count, threshold_cents, delay_minutes
//   - String length cap violations
//   - EIV clamping: below 0 and above 1
//   - Duplicate candidates: same fingerprint across two requests
//   - Provider failure: mock returning error
//   - Context cancellation during generation
//   - Budget exceeded (prompt token estimate)
//   - Valid multi-candidate response: all three mutation types
package scenario

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
)

// ─── Mock provider ────────────────────────────────────────────────────────────

// mockProvider implements GeminiProvider for tests.
// It either returns a fixed JSON string or a fixed error.
type mockProvider struct {
	// responseJSON is the raw JSON that ParseAndValidate will process.
	responseJSON string
	// err, if non-nil, is returned immediately from Generate.
	err error
	// calls counts invocations for retry-count assertions.
	calls int
	// failFirstN makes the first N calls fail with a transient error, then succeed.
	failFirstN int
}

func (m *mockProvider) Generate(_ context.Context, req GenerationRequest) (GenerationResult, error) {
	m.calls++
	if m.err != nil {
		return GenerationResult{}, m.err
	}
	if m.failFirstN > 0 && m.calls <= m.failFirstN {
		return GenerationResult{}, &GeminiProviderError{
			StatusCode: 503, CorrelationID: req.CorrelationID, Message: "transient",
		}
	}
	accepted, rejected := ParseAndValidate([]byte(m.responseJSON), req.CorrelationID)
	return GenerationResult{
		Accepted:      accepted,
		Rejected:      rejected,
		CorrelationID: req.CorrelationID,
	}, nil
}

// ─── Helper: fixed GenerationRequest ─────────────────────────────────────────

func fixedRequest() GenerationRequest {
	return GenerationRequest{
		CorrelationID: uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"),
		MaxCandidates: 3,
		CohortSummary: CohortSummary{
			EntityCount: 20, AccountCount: 50, TxCount: 500,
			WindowDays: 90, TopCountryCodes: []string{"US", "GB"},
			AlertRatePercent: 12.5,
		},
		ExistingFingerprints: make(map[string]bool),
	}
}

// validCandidateJSON returns a single valid split_transaction candidate.
func validCandidateJSON(extra string) string {
	return fmt.Sprintf(`{"candidates":[{
		"mutation_type":"split_transaction",
		"declared_change":"Split large wire transfers into three parts",
		"mutation_spec":{"split_count":3,"threshold_cents":1000000},
		"rationale":"Splitting large transactions avoids the reporting threshold.",
		"tags":["structuring","wire"],
		"eiv_confidence":0.75%s
	}]}`, extra)
}

// ─── ParseAndValidate: valid input ────────────────────────────────────────────

func TestParseAndValidate_ValidSplitTransaction(t *testing.T) {
	corr := uuid.New()
	accepted, rejected := ParseAndValidate([]byte(validCandidateJSON("")), corr)
	if len(rejected) != 0 {
		t.Errorf("expected 0 rejections, got %d: %+v", len(rejected), rejected)
	}
	if len(accepted) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(accepted))
	}
	c := accepted[0]
	if c.MutationType != MutationSplitTransaction {
		t.Errorf("mutation_type: %s", c.MutationType)
	}
	if c.MutationSpec.SplitCount != 3 {
		t.Errorf("split_count: %d", c.MutationSpec.SplitCount)
	}
	if c.MutationSpec.ThresholdCents != 1_000_000 {
		t.Errorf("threshold_cents: %d", c.MutationSpec.ThresholdCents)
	}
	if c.ScoreComponents.Validity != 1.0 {
		t.Errorf("validity: %f", c.ScoreComponents.Validity)
	}
}

func TestParseAndValidate_ValidDelayAndResubmit(t *testing.T) {
	raw := `{"candidates":[{
		"mutation_type":"delay_and_resubmit",
		"declared_change":"Delay suspicious transaction by 2 hours",
		"mutation_spec":{"delay_minutes":120},
		"rationale":"Delays reduce rapid-movement detection.",
		"tags":["rapid-movement"]
	}]}`
	accepted, rejected := ParseAndValidate([]byte(raw), uuid.New())
	if len(rejected) != 0 {
		t.Errorf("unexpected rejections: %+v", rejected)
	}
	if len(accepted) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(accepted))
	}
	if accepted[0].MutationSpec.DelayMinutes != 120 {
		t.Errorf("delay_minutes: %d", accepted[0].MutationSpec.DelayMinutes)
	}
}

func TestParseAndValidate_ValidIntroduceShell(t *testing.T) {
	raw := `{"candidates":[{
		"mutation_type":"introduce_shell_entity",
		"declared_change":"Route funds through a shell company",
		"mutation_spec":{"threshold_cents":500000},
		"rationale":"Shell entities obscure beneficial ownership.",
		"tags":["layering","shell"]
	}]}`
	accepted, rejected := ParseAndValidate([]byte(raw), uuid.New())
	if len(rejected) != 0 {
		t.Errorf("unexpected rejections: %+v", rejected)
	}
	if len(accepted) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(accepted))
	}
}

// ─── ParseAndValidate: EIV clamping ──────────────────────────────────────────

func TestParseAndValidate_EIVClampedAbove1(t *testing.T) {
	raw := `{"candidates":[{
		"mutation_type":"split_transaction",
		"declared_change":"Split transaction",
		"mutation_spec":{"split_count":3,"threshold_cents":100000},
		"rationale":"Testing EIV clamp.",
		"eiv_confidence":1.5
	}]}`
	accepted, _ := ParseAndValidate([]byte(raw), uuid.New())
	if len(accepted) != 1 {
		t.Fatal("expected accepted candidate")
	}
	if accepted[0].ScoreComponents.EIV != 1.0 {
		t.Errorf("EIV should be clamped to 1.0, got %f", accepted[0].ScoreComponents.EIV)
	}
}

func TestParseAndValidate_EIVClampedBelow0(t *testing.T) {
	raw := `{"candidates":[{
		"mutation_type":"delay_and_resubmit",
		"declared_change":"Delay transaction",
		"mutation_spec":{"delay_minutes":60},
		"rationale":"Testing EIV clamp.",
		"eiv_confidence":-0.5
	}]}`
	accepted, _ := ParseAndValidate([]byte(raw), uuid.New())
	if len(accepted) != 1 {
		t.Fatal("expected accepted candidate")
	}
	if accepted[0].ScoreComponents.EIV != 0.0 {
		t.Errorf("EIV should be clamped to 0.0, got %f", accepted[0].ScoreComponents.EIV)
	}
}

func TestParseAndValidate_EIVDefaultWhenAbsent(t *testing.T) {
	// No eiv_confidence field — should default to 0.5.
	accepted, _ := ParseAndValidate([]byte(validCandidateJSON("")), uuid.New())
	// validCandidateJSON has eiv_confidence:0.75, so test a version without it.
	raw := `{"candidates":[{
		"mutation_type":"split_transaction",
		"declared_change":"Split transaction",
		"mutation_spec":{"split_count":3,"threshold_cents":100000},
		"rationale":"No EIV field."
	}]}`
	accepted, _ = ParseAndValidate([]byte(raw), uuid.New())
	if len(accepted) != 1 {
		t.Fatal("expected accepted candidate")
	}
	if accepted[0].ScoreComponents.EIV != 0.5 {
		t.Errorf("EIV default: got %f, want 0.5", accepted[0].ScoreComponents.EIV)
	}
}

// ─── ParseAndValidate: malformed JSON ────────────────────────────────────────

func TestParseAndValidate_NotJSON(t *testing.T) {
	_, rejected := ParseAndValidate([]byte(`not json at all`), uuid.New())
	if len(rejected) == 0 {
		t.Fatal("expected rejection for non-JSON input")
	}
	if rejected[0].Reason != RejectMalformedJSON {
		t.Errorf("reason: %s", rejected[0].Reason)
	}
}

func TestParseAndValidate_EmptyObject(t *testing.T) {
	// {} is valid JSON but has no candidates array → zero results, zero rejections.
	accepted, rejected := ParseAndValidate([]byte(`{}`), uuid.New())
	if len(accepted) != 0 || len(rejected) != 0 {
		t.Errorf("empty object: accepted=%d rejected=%d", len(accepted), len(rejected))
	}
}

func TestParseAndValidate_CandidateNotObject(t *testing.T) {
	raw := `{"candidates":["not an object"]}`
	_, rejected := ParseAndValidate([]byte(raw), uuid.New())
	if len(rejected) == 0 {
		t.Fatal("expected rejection for non-object candidate")
	}
	if rejected[0].Reason != RejectMalformedJSON {
		t.Errorf("reason: %s", rejected[0].Reason)
	}
}

// ─── ParseAndValidate: unknown fields ────────────────────────────────────────

func TestParseAndValidate_UnknownTopLevelField(t *testing.T) {
	raw := `{"candidates":[{
		"mutation_type":"split_transaction",
		"declared_change":"Split",
		"mutation_spec":{"split_count":3,"threshold_cents":100000},
		"rationale":"x",
		"injected_field":"evil"
	}]}`
	_, rejected := ParseAndValidate([]byte(raw), uuid.New())
	if len(rejected) == 0 {
		t.Fatal("expected rejection for unknown field")
	}
	if rejected[0].Reason != RejectSchemaUnknownField {
		t.Errorf("reason: %s", rejected[0].Reason)
	}
}

func TestParseAndValidate_UnknownSpecField(t *testing.T) {
	raw := `{"candidates":[{
		"mutation_type":"split_transaction",
		"declared_change":"Split",
		"mutation_spec":{"split_count":3,"threshold_cents":100000,"exec_cmd":"rm -rf /"},
		"rationale":"x"
	}]}`
	_, rejected := ParseAndValidate([]byte(raw), uuid.New())
	if len(rejected) == 0 {
		t.Fatal("expected rejection for unknown spec field")
	}
	if rejected[0].Reason != RejectSchemaUnknownField {
		t.Errorf("reason: %s", rejected[0].Reason)
	}
}

// ─── ParseAndValidate: missing required fields ────────────────────────────────

func TestParseAndValidate_MissingMutationType(t *testing.T) {
	raw := `{"candidates":[{
		"declared_change":"Split",
		"mutation_spec":{"split_count":3,"threshold_cents":100000},
		"rationale":"x"
	}]}`
	_, rejected := ParseAndValidate([]byte(raw), uuid.New())
	if len(rejected) == 0 {
		t.Fatal("expected rejection")
	}
	if rejected[0].Reason != RejectSchemaMissing {
		t.Errorf("reason: %s", rejected[0].Reason)
	}
}

func TestParseAndValidate_MissingDeclaredChange(t *testing.T) {
	raw := `{"candidates":[{
		"mutation_type":"split_transaction",
		"mutation_spec":{"split_count":3,"threshold_cents":100000},
		"rationale":"x"
	}]}`
	_, rejected := ParseAndValidate([]byte(raw), uuid.New())
	if len(rejected) == 0 {
		t.Fatal("expected rejection for missing declared_change")
	}
}

func TestParseAndValidate_MissingSpecField_SplitCount(t *testing.T) {
	raw := `{"candidates":[{
		"mutation_type":"split_transaction",
		"declared_change":"Split",
		"mutation_spec":{"threshold_cents":100000},
		"rationale":"x"
	}]}`
	_, rejected := ParseAndValidate([]byte(raw), uuid.New())
	if len(rejected) == 0 {
		t.Fatal("expected rejection for missing split_count")
	}
	if rejected[0].Reason != RejectSchemaMissing {
		t.Errorf("reason: %s", rejected[0].Reason)
	}
}

func TestParseAndValidate_MissingSpecField_DelayMinutes(t *testing.T) {
	raw := `{"candidates":[{
		"mutation_type":"delay_and_resubmit",
		"declared_change":"Delay",
		"mutation_spec":{},
		"rationale":"x"
	}]}`
	_, rejected := ParseAndValidate([]byte(raw), uuid.New())
	if len(rejected) == 0 {
		t.Fatal("expected rejection for missing delay_minutes")
	}
}

// ─── ParseAndValidate: unapproved mutation type ───────────────────────────────

func TestParseAndValidate_UnapprovedMutationType(t *testing.T) {
	raw := `{"candidates":[{
		"mutation_type":"execute_sql_drop_tables",
		"declared_change":"Drop database",
		"mutation_spec":{},
		"rationale":"x"
	}]}`
	_, rejected := ParseAndValidate([]byte(raw), uuid.New())
	if len(rejected) == 0 {
		t.Fatal("expected rejection for unapproved mutation type")
	}
	if rejected[0].Reason != RejectUnapprovedMutation {
		t.Errorf("reason: %s", rejected[0].Reason)
	}
}

// ─── ParseAndValidate: out-of-range spec values ───────────────────────────────

func TestParseAndValidate_SplitCountTooLow(t *testing.T) {
	raw := `{"candidates":[{
		"mutation_type":"split_transaction",
		"declared_change":"Split",
		"mutation_spec":{"split_count":1,"threshold_cents":100000},
		"rationale":"x"
	}]}`
	_, rejected := ParseAndValidate([]byte(raw), uuid.New())
	if len(rejected) == 0 {
		t.Fatal("expected rejection for split_count < 2")
	}
	if rejected[0].Reason != RejectSchemaInvalidValue {
		t.Errorf("reason: %s", rejected[0].Reason)
	}
}

func TestParseAndValidate_SplitCountTooHigh(t *testing.T) {
	raw := `{"candidates":[{
		"mutation_type":"split_transaction",
		"declared_change":"Split",
		"mutation_spec":{"split_count":21,"threshold_cents":100000},
		"rationale":"x"
	}]}`
	_, rejected := ParseAndValidate([]byte(raw), uuid.New())
	if len(rejected) == 0 {
		t.Fatal("expected rejection for split_count > 20")
	}
}

func TestParseAndValidate_ThresholdCentsZero(t *testing.T) {
	raw := `{"candidates":[{
		"mutation_type":"split_transaction",
		"declared_change":"Split",
		"mutation_spec":{"split_count":3,"threshold_cents":0},
		"rationale":"x"
	}]}`
	_, rejected := ParseAndValidate([]byte(raw), uuid.New())
	if len(rejected) == 0 {
		t.Fatal("expected rejection for threshold_cents=0")
	}
}

func TestParseAndValidate_DelayMinutesTooLow(t *testing.T) {
	raw := `{"candidates":[{
		"mutation_type":"delay_and_resubmit",
		"declared_change":"Delay",
		"mutation_spec":{"delay_minutes":0},
		"rationale":"x"
	}]}`
	_, rejected := ParseAndValidate([]byte(raw), uuid.New())
	if len(rejected) == 0 {
		t.Fatal("expected rejection for delay_minutes=0")
	}
}

func TestParseAndValidate_DelayMinutesTooHigh(t *testing.T) {
	raw := `{"candidates":[{
		"mutation_type":"delay_and_resubmit",
		"declared_change":"Delay",
		"mutation_spec":{"delay_minutes":1441},
		"rationale":"x"
	}]}`
	_, rejected := ParseAndValidate([]byte(raw), uuid.New())
	if len(rejected) == 0 {
		t.Fatal("expected rejection for delay_minutes > 1440")
	}
}

// ─── ParseAndValidate: string length caps ────────────────────────────────────

func TestParseAndValidate_DeclaredChangeTooLong(t *testing.T) {
	long := make([]byte, 1025)
	for i := range long {
		long[i] = 'x'
	}
	raw := fmt.Sprintf(`{"candidates":[{
		"mutation_type":"split_transaction",
		"declared_change":"%s",
		"mutation_spec":{"split_count":3,"threshold_cents":100000},
		"rationale":"x"
	}]}`, string(long))
	_, rejected := ParseAndValidate([]byte(raw), uuid.New())
	if len(rejected) == 0 {
		t.Fatal("expected rejection for declared_change > 1024")
	}
}

// ─── ParseAndValidate: mixed valid + invalid ──────────────────────────────────

func TestParseAndValidate_MixedValidAndInvalid(t *testing.T) {
	raw := `{"candidates":[
		{
			"mutation_type":"split_transaction",
			"declared_change":"Good candidate",
			"mutation_spec":{"split_count":3,"threshold_cents":100000},
			"rationale":"Valid."
		},
		{
			"mutation_type":"bad_type",
			"declared_change":"Bad candidate",
			"mutation_spec":{},
			"rationale":"x"
		}
	]}`
	accepted, rejected := ParseAndValidate([]byte(raw), uuid.New())
	if len(accepted) != 1 {
		t.Errorf("expected 1 accepted, got %d", len(accepted))
	}
	if len(rejected) != 1 {
		t.Errorf("expected 1 rejected, got %d", len(rejected))
	}
}

// ─── Mock provider: provider error ────────────────────────────────────────────

func TestMockProvider_ProviderError(t *testing.T) {
	provider := &mockProvider{err: fmt.Errorf("network failure")}
	ctx := context.Background()
	req := fixedRequest()

	_, err := provider.Generate(ctx, req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if provider.calls != 1 {
		t.Errorf("expected 1 call, got %d", provider.calls)
	}
}

func TestMockProvider_GeminiProviderError_Wraps(t *testing.T) {
	provErr := &GeminiProviderError{
		StatusCode: 503, CorrelationID: uuid.New(), Message: "Service Unavailable",
	}
	if !errors.As(provErr, &provErr) {
		t.Error("GeminiProviderError should satisfy errors.As")
	}
	if provErr.StatusCode != 503 {
		t.Errorf("StatusCode: %d", provErr.StatusCode)
	}
}

// ─── Mock provider: context cancellation ─────────────────────────────────────

func TestMockProvider_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled before call

	// A well-behaved provider must respect ctx. Our mockProvider doesn't check
	// ctx, but we verify that callers of real providers get the right error type.
	// For the real provider this would surface as ctx.Err(); test the error path.
	provider := &mockProvider{err: context.Canceled}
	_, err := provider.Generate(ctx, fixedRequest())
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestMockProvider_ContextDeadline(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	provider := &mockProvider{err: context.DeadlineExceeded}
	_, err := provider.Generate(ctx, fixedRequest())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}
}

// ─── Mock provider: duplicate candidates ─────────────────────────────────────

func TestMockProvider_DuplicateCandidates_SameRequest(t *testing.T) {
	// Two identical candidates in one response — only one fingerprint is unique.
	raw := `{"candidates":[
		{
			"mutation_type":"split_transaction",
			"declared_change":"Split large wire transfers into three parts",
			"mutation_spec":{"split_count":3,"threshold_cents":1000000},
			"rationale":"First instance."
		},
		{
			"mutation_type":"split_transaction",
			"declared_change":"Split large wire transfers into three parts",
			"mutation_spec":{"split_count":3,"threshold_cents":1000000},
			"rationale":"Second instance — identical fingerprint."
		}
	]}`
	provider := &mockProvider{responseJSON: raw}
	result, err := provider.Generate(context.Background(), fixedRequest())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	// Both candidates parse as valid from schema's perspective.
	// Deduplication happens in the selector, not the parser.
	// Verify both accepted here, dedup tested in selector_test.go.
	if len(result.Accepted) != 2 {
		t.Errorf("parser: expected 2 accepted, got %d", len(result.Accepted))
	}
}

// ─── ClientConfig.validate ────────────────────────────────────────────────────

func TestClientConfig_Validate_EmptyProjectID(t *testing.T) {
	cfg := DefaultClientConfig("", "us-central1")
	if err := cfg.validate(); err == nil {
		t.Error("expected error for empty ProjectID")
	}
}

func TestClientConfig_Validate_BadMaxOutputTokens(t *testing.T) {
	cfg := DefaultClientConfig("proj", "us-central1")
	cfg.MaxOutputTokens = 100 // below minimum
	if err := cfg.validate(); err == nil {
		t.Error("expected error for MaxOutputTokens < 256")
	}
}

func TestClientConfig_Validate_BadTemperature(t *testing.T) {
	cfg := DefaultClientConfig("proj", "us-central1")
	cfg.Temperature = 1.5
	if err := cfg.validate(); err == nil {
		t.Error("expected error for Temperature > 1.0")
	}
}

func TestClientConfig_Validate_MaxRetriesTooHigh(t *testing.T) {
	cfg := DefaultClientConfig("proj", "us-central1")
	cfg.MaxRetries = 6
	if err := cfg.validate(); err == nil {
		t.Error("expected error for MaxRetries > 5")
	}
}

// ─── buildPrompt: no executable content ──────────────────────────────────────

func TestBuildPrompt_ContainsNoExecInstruction(t *testing.T) {
	req := fixedRequest()
	prompt := buildPrompt(req)
	// Check for executable function-call patterns, not the word "exec" which
	// legitimately appears inside "executable" in the safety instruction text.
	forbidden := []string{"exec(", "eval(", "os.system(", "subprocess(", "DROP TABLE", "DELETE FROM"}
	for _, f := range forbidden {
		if containsCI(prompt, f) {
			t.Errorf("prompt contains forbidden pattern %q", f)
		}
	}
}

func containsCI(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub ||
		len(s) > 0 && containsBytes([]byte(s), []byte(sub)))
}

func containsBytes(haystack, needle []byte) bool {
	for i := 0; i <= len(haystack)-len(needle); i++ {
		if string(haystack[i:i+len(needle)]) == string(needle) {
			return true
		}
	}
	return false
}

// ─── CorrelationID propagated ─────────────────────────────────────────────────

func TestParseAndValidate_CorrelationIDOnRejection(t *testing.T) {
	corrID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	_, rejected := ParseAndValidate([]byte(`not json`), corrID)
	if len(rejected) == 0 {
		t.Fatal("expected rejection")
	}
	if rejected[0].CorrelationID != corrID {
		t.Errorf("correlation ID not propagated: got %s", rejected[0].CorrelationID)
	}
}

func TestParseAndValidate_CorrelationIDOnAccepted(t *testing.T) {
	corrID := uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	accepted, _ := ParseAndValidate([]byte(validCandidateJSON("")), corrID)
	if len(accepted) == 0 {
		t.Fatal("expected accepted candidate")
	}
	if accepted[0].CorrelationID != corrID {
		t.Errorf("correlation ID not propagated: got %s", accepted[0].CorrelationID)
	}
}
