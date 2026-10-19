package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/yourorg/financial-crime-red-team/internal/counterfactual"
	"github.com/yourorg/financial-crime-red-team/internal/genome"
)

func testGenome() genome.Genome {
	g := genome.Genome{
		SchemaVersion: genome.SchemaVersion,
		SyntheticOnly: true,
		Dimensions: genome.Dimensions{
			Actors:            "company",
			Accounts:          "active",
			Transactions:      "structuring",
			TransactionCount:  6,
			Temporal:          "burst",
			Network:           "star",
			CounterpartyCount: 3,
			Ownership:         "single_controller",
			Geography:         "domestic",
			BehavioralChange:  "increase_velocity",
			ControlContext:    "velocity",
		},
	}
	g.GenomeID = g.Fingerprint()
	return g
}

func counterfactualRouter() http.Handler {
	r := chi.NewRouter()
	h := NewCounterfactualHandler()
	r.Post("/counterfactuals/validate", h.Validate)
	return r
}

func postJSON(t *testing.T, r http.Handler, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/counterfactuals/validate", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestCounterfactualValidate_TightlyControlledPair(t *testing.T) {
	baseline := testGenome()
	variant := testGenome()
	variant.Dimensions.TransactionCount = 12
	variant.GenomeID = variant.Fingerprint()

	rec := postJSON(t, counterfactualRouter(), counterfactualRequest{
		Baseline:           baseline,
		Variant:            variant,
		DeclaredDimensions: []string{genome.DimTransactions},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Data counterfactual.Report `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	rep := got.Data
	if !rep.Valid || rep.Classification != counterfactual.TightlyControlled {
		t.Fatalf("valid=%v classification=%q, want valid tightly_controlled", rep.Valid, rep.Classification)
	}
}

func TestCounterfactualValidate_UnintendedChangeIsReportedInvalid(t *testing.T) {
	baseline := testGenome()
	variant := testGenome()
	variant.Dimensions.TransactionCount = 12
	variant.Dimensions.Geography = "high_risk_jurisdiction"
	variant.GenomeID = variant.Fingerprint()

	rec := postJSON(t, counterfactualRouter(), counterfactualRequest{
		Baseline:           baseline,
		Variant:            variant,
		DeclaredDimensions: []string{genome.DimTransactions},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Data counterfactual.Report `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	rep := got.Data
	if rep.Valid || rep.Classification != counterfactual.Invalid {
		t.Fatalf("valid=%v classification=%q, want invalid", rep.Valid, rep.Classification)
	}
	if len(rep.Violations) == 0 {
		t.Fatal("violations empty, want the unintended geography change")
	}
}

func TestCounterfactualValidate_MissingDeclaredDimensions(t *testing.T) {
	rec := postJSON(t, counterfactualRouter(), counterfactualRequest{
		Baseline: testGenome(),
		Variant:  testGenome(),
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s, want 422", rec.Code, rec.Body.String())
	}
}

func TestCounterfactualValidate_MalformedGenome(t *testing.T) {
	rec := postJSON(t, counterfactualRouter(), counterfactualRequest{
		Baseline:           genome.Genome{},
		Variant:            testGenome(),
		DeclaredDimensions: []string{genome.DimTransactions},
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s, want 422", rec.Code, rec.Body.String())
	}
}
