// Package scenario — schema.go
//
// Schema validation for raw Gemini output. This is the security boundary
// between untrusted model output and the rest of the system.
//
// Rules:
//   - Only approved top-level fields are accepted; unknown fields → rejection.
//   - mutation_type must be an exact member of ApprovedMutationTypes.
//   - mutation_spec fields are type-checked and range-validated.
//   - declared_change and rationale are treated as display-only strings; they
//     are never executed, interpolated into SQL, or used as code.
//   - No generated content is ever eval'd, passed to exec.Command, or used
//     as a format string.
package scenario

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// approvedTopLevelFields is the exhaustive set of JSON keys allowed in a
// raw Gemini candidate response object. Any additional key causes rejection.
var approvedTopLevelFields = map[string]bool{
	"mutation_type":   true,
	"declared_change": true,
	"mutation_spec":   true,
	"rationale":       true,
	"tags":            true,
	"eiv_confidence":  true, // optional: model-reported EIV, float in [0,1]
}

// approvedSpecFields is the exhaustive set of JSON keys allowed inside
// the mutation_spec object.
var approvedSpecFields = map[string]bool{
	"split_count":     true,
	"threshold_cents": true,
	"delay_minutes":   true,
}

// rawCandidate is the intermediate, unvalidated representation of a Gemini
// candidate. It is decoded via DisallowUnknownFields to catch extra keys.
// IMPORTANT: this type must NEVER be used outside schema.go; callers work
// only with the validated Candidate type.
type rawCandidate struct {
	MutationType   string         `json:"mutation_type"`
	DeclaredChange string         `json:"declared_change"`
	MutationSpec   map[string]any `json:"mutation_spec"` // any: intentional; validated field-by-field below
	Rationale      string         `json:"rationale"`
	Tags           []string       `json:"tags,omitempty"`
	EIVConfidence  *float64       `json:"eiv_confidence,omitempty"` // pointer: distinguish absent from 0.0
}

// rawResponse wraps the array of candidates Gemini returns.
type rawResponse struct {
	Candidates []json.RawMessage `json:"candidates"`
}

// ParseAndValidate parses a raw JSON bytes value from the Gemini response,
// validates every field against the approved schema, and returns a slice of
// validated Candidates plus a RejectionRecord for each discarded item.
//
// Security invariants:
//   - Uses json.Decoder with DisallowUnknownFields for the spec sub-object.
//   - Each top-level key is checked against approvedTopLevelFields before use.
//   - mutation_type is checked against ApprovedMutationTypes.
//   - String fields are length-capped to prevent memory exhaustion.
//   - EIV confidence is clamped to [0,1] after parse.
func ParseAndValidate(raw []byte, correlationID uuid.UUID) ([]Candidate, []RejectionRecord) {
	var accepted []Candidate
	var rejected []RejectionRecord

	// Outer parse: expect {"candidates": [...]}
	var resp rawResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		// Fallback: try treating raw as a direct array.
		var arr []json.RawMessage
		if err2 := json.Unmarshal(raw, &arr); err2 != nil {
			rejected = append(rejected, RejectionRecord{
				CorrelationID: correlationID,
				Reason:        RejectMalformedJSON,
				Detail:        "response is neither {candidates:[...]} nor a JSON array",
				RejectedAt:    time.Now(),
			})
			return nil, rejected
		}
		resp.Candidates = arr
	}

	for i, rawItem := range resp.Candidates {
		cand, rec := validateOne(rawItem, correlationID, i)
		if rec != nil {
			rejected = append(rejected, *rec)
		} else {
			accepted = append(accepted, *cand)
		}
	}
	return accepted, rejected
}

// validateOne validates a single raw JSON item and returns either a
// validated Candidate or a RejectionRecord (never both, never neither).
func validateOne(raw json.RawMessage, correlationID uuid.UUID, idx int) (*Candidate, *RejectionRecord) {
	reject := func(reason RejectionReason, detail string) *RejectionRecord {
		return &RejectionRecord{
			CorrelationID: correlationID,
			Reason:        reason,
			Detail:        fmt.Sprintf("candidate[%d]: %s", idx, detail),
			RejectedAt:    time.Now(),
		}
	}

	// Step 1: parse into a generic map to check for unknown top-level keys.
	var topLevel map[string]json.RawMessage
	if err := json.Unmarshal(raw, &topLevel); err != nil {
		return nil, reject(RejectMalformedJSON, "not a JSON object")
	}
	for k := range topLevel {
		if !approvedTopLevelFields[k] {
			return nil, reject(RejectSchemaUnknownField,
				fmt.Sprintf("unknown field %q not in approved schema", k))
		}
	}

	// Step 2: parse into typed struct.
	var rc rawCandidate
	if err := json.Unmarshal(raw, &rc); err != nil {
		return nil, reject(RejectMalformedJSON, "struct decode failed: "+sanitise(err.Error()))
	}

	// Step 3: required fields.
	if rc.MutationType == "" {
		return nil, reject(RejectSchemaMissing, "mutation_type is required")
	}
	if rc.DeclaredChange == "" {
		return nil, reject(RejectSchemaMissing, "declared_change is required")
	}
	if rc.Rationale == "" {
		return nil, reject(RejectSchemaMissing, "rationale is required")
	}

	// Step 4: mutation_type allowlist.
	mt := MutationType(rc.MutationType)
	if !ApprovedMutationTypes[mt] {
		return nil, reject(RejectUnapprovedMutation,
			fmt.Sprintf("mutation_type %q is not in the approved list", rc.MutationType))
	}

	// Step 5: string length caps (defence against memory exhaustion).
	if len(rc.DeclaredChange) > 1024 {
		return nil, reject(RejectSchemaInvalidValue, "declared_change exceeds 1024 chars")
	}
	if len(rc.Rationale) > 4096 {
		return nil, reject(RejectSchemaInvalidValue, "rationale exceeds 4096 chars")
	}
	if len(rc.Tags) > 20 {
		return nil, reject(RejectSchemaInvalidValue, "tags list exceeds 20 entries")
	}
	for _, tag := range rc.Tags {
		if len(tag) > 64 {
			return nil, reject(RejectSchemaInvalidValue, "tag exceeds 64 chars")
		}
	}

	// Step 6: mutation_spec field allowlist + type + range validation.
	spec, rec := validateSpec(rc.MutationSpec, mt, correlationID, idx)
	if rec != nil {
		return nil, rec
	}

	// Step 7: EIV confidence clamp.
	eiv := 0.5 // default when absent
	if rc.EIVConfidence != nil {
		eiv = clamp01(*rc.EIVConfidence)
	}

	cand := &Candidate{
		ID:             uuid.New(),
		CorrelationID:  correlationID,
		MutationType:   mt,
		DeclaredChange: rc.DeclaredChange,
		MutationSpec:   spec,
		Rationale:      rc.Rationale,
		Tags:           rc.Tags,
		GeneratedAt:    time.Now(),
		ScoreComponents: ScoreComponents{
			Validity: 1.0,
			EIV:      eiv,
		},
	}
	return cand, nil
}

// validateSpec validates the mutation_spec sub-object for allowed keys, types,
// and ranges, returning a typed MutationSpec or a RejectionRecord.
func validateSpec(
	raw map[string]any, // any: open model output; validated field-by-field here
	mt MutationType,
	correlationID uuid.UUID,
	idx int,
) (MutationSpec, *RejectionRecord) {
	reject := func(reason RejectionReason, detail string) *RejectionRecord {
		return &RejectionRecord{
			CorrelationID: correlationID,
			Reason:        reason,
			Detail:        fmt.Sprintf("candidate[%d].mutation_spec: %s", idx, detail),
			RejectedAt:    time.Now(),
		}
	}

	// Check for unknown fields in spec.
	for k := range raw {
		if !approvedSpecFields[k] {
			return MutationSpec{}, reject(RejectSchemaUnknownField,
				fmt.Sprintf("unknown spec field %q", k))
		}
	}

	var spec MutationSpec

	switch mt {
	case MutationSplitTransaction:
		// Required: split_count, threshold_cents.
		sc, err := intFromMap(raw, "split_count")
		if err != nil {
			return MutationSpec{}, reject(RejectSchemaMissing, "split_count: "+err.Error())
		}
		if sc < 2 || sc > 20 {
			return MutationSpec{}, reject(RejectSchemaInvalidValue,
				fmt.Sprintf("split_count %d out of range [2,20]", sc))
		}
		tc, err := int64FromMap(raw, "threshold_cents")
		if err != nil {
			return MutationSpec{}, reject(RejectSchemaMissing, "threshold_cents: "+err.Error())
		}
		if tc <= 0 {
			return MutationSpec{}, reject(RejectSchemaInvalidValue, "threshold_cents must be > 0")
		}
		spec.SplitCount = sc
		spec.ThresholdCents = tc

	case MutationDelayAndResubmit:
		// Required: delay_minutes.
		dm, err := intFromMap(raw, "delay_minutes")
		if err != nil {
			return MutationSpec{}, reject(RejectSchemaMissing, "delay_minutes: "+err.Error())
		}
		if dm < 1 || dm > 1440 {
			return MutationSpec{}, reject(RejectSchemaInvalidValue,
				fmt.Sprintf("delay_minutes %d out of range [1,1440]", dm))
		}
		spec.DelayMinutes = dm

	case MutationIntroduceShell:
		// Required: threshold_cents.
		tc, err := int64FromMap(raw, "threshold_cents")
		if err != nil {
			return MutationSpec{}, reject(RejectSchemaMissing, "threshold_cents: "+err.Error())
		}
		if tc <= 0 {
			return MutationSpec{}, reject(RejectSchemaInvalidValue, "threshold_cents must be > 0")
		}
		spec.ThresholdCents = tc
	}

	return spec, nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func intFromMap(m map[string]any, key string) (int, error) {
	v, ok := m[key]
	if !ok {
		return 0, fmt.Errorf("missing required field")
	}
	switch n := v.(type) {
	case float64:
		return int(n), nil
	case int:
		return n, nil
	case int64:
		return int(n), nil
	case json.Number:
		i, err := n.Int64()
		return int(i), err
	}
	return 0, fmt.Errorf("expected numeric, got %T", v)
}

func int64FromMap(m map[string]any, key string) (int64, error) {
	v, ok := m[key]
	if !ok {
		return 0, fmt.Errorf("missing required field")
	}
	switch n := v.(type) {
	case float64:
		return int64(n), nil
	case int64:
		return n, nil
	case int:
		return int64(n), nil
	case json.Number:
		return n.Int64()
	}
	return 0, fmt.Errorf("expected numeric, got %T", v)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// sanitise removes newlines and truncates an error message so it is safe
// to include in structured log output.
func sanitise(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	if len(s) > 256 {
		return s[:256] + "…"
	}
	return s
}
