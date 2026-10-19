// Package api — handler_scenario_test.go
//
// Integration tests for scenario creation: POST /api/v1/scenarios stores a
// proposal produced by the Gemini provider, and the stored proposal is
// readable again. Gemini itself is replaced by a stub, so the test is about
// the handler's validation, cohort gating and persistence rather than the
// model.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yourorg/financial-crime-red-team/internal/bank"
	"github.com/yourorg/financial-crime-red-team/internal/scenario"
)

// ─── Harness ──────────────────────────────────────────────────────────────────

func scenarioAPITestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_TEST_URL")
	if dsn == "" {
		t.Skip("DATABASE_TEST_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// seedScenarioCohort inserts a cohort directly; the API has no create-cohort
// endpoint in these tests and the handler only needs the stored row.
func seedScenarioCohort(t *testing.T, pool *pgxpool.Pool, locked bool) uuid.UUID {
	t.Helper()
	manifest := `{"entity_count":12,"account_count":30,"tx_count":250}`
	var id uuid.UUID
	err := pool.QueryRow(context.Background(), `
		INSERT INTO cohort (name, predicate, seed, generator_version, manifest, locked_at)
		VALUES ($1, '{}', 7, 'test-v1', $2::jsonb, CASE WHEN $3 THEN NOW() ELSE NULL END)
		RETURNING id`, "scenario-api-test-"+uuid.NewString(), manifest, locked).Scan(&id)
	if err != nil {
		t.Fatalf("seed cohort: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `DELETE FROM scenario_proposal WHERE cohort_id = $1`, id)
		_, _ = pool.Exec(ctx, `DELETE FROM cohort WHERE id = $1`, id)
	})
	return id
}

type stubGeminiProvider struct {
	result scenario.GenerationResult
	err    error
	req    scenario.GenerationRequest
}

func (s *stubGeminiProvider) Generate(_ context.Context, req scenario.GenerationRequest) (scenario.GenerationResult, error) {
	s.req = req
	return s.result, s.err
}

func acceptedResult() scenario.GenerationResult {
	return scenario.GenerationResult{
		ModelID: "gemini-test-model",
		Accepted: []scenario.Candidate{{
			ID:             uuid.New(),
			MutationType:   scenario.MutationSplitTransaction,
			DeclaredChange: "Split a large transfer into three parts below the reporting threshold",
			MutationSpec:   scenario.MutationSpec{SplitCount: 3, ThresholdCents: 100000},
			Rationale:      "Structuring evades the aggregate-threshold control.",
		}},
	}
}

func scenarioRouter(h *ScenarioHandler) http.Handler {
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/scenarios", h.RequestScenario)
		r.Get("/scenarios/{id}", h.GetScenario)
	})
	return r
}

// ─── Tests ────────────────────────────────────────────────────────────────────

func TestScenarioHandler_CreatePersistsProposal(t *testing.T) {
	pool := scenarioAPITestPool(t)
	cohortID := seedScenarioCohort(t, pool, true)

	provider := &stubGeminiProvider{result: acceptedResult()}
	h := NewScenarioHandler(scenario.NewRepository(pool), bank.NewRepository(pool), nil, provider)
	router := scenarioRouter(h)

	rec := call(router, http.MethodPost, "/api/v1/scenarios",
		`{"cohort_id":"`+cohortID.String()+`","max_candidates":2}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("got %d, want 201 (%s)", rec.Code, rec.Body.String())
	}

	var env struct {
		Data scenario.Proposal `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode proposal: %v", err)
	}
	p := env.Data
	if p.Status != scenario.StatusPending {
		t.Errorf("status=%q, want pending", p.Status)
	}
	if p.ScenarioType != string(scenario.MutationSplitTransaction) {
		t.Errorf("scenario_type=%q", p.ScenarioType)
	}
	if p.Description == "" || p.ExpectedEvasionVector == "" {
		t.Error("description and rationale must be stored")
	}
	if p.GeminiModel != "gemini-test-model" {
		t.Errorf("gemini_model=%q, want the producing model", p.GeminiModel)
	}
	if p.CorrelationID == uuid.Nil {
		t.Error("correlation_id must link the proposal to the Gemini call")
	}
	if len(p.SuggestedTxPattern) != 1 {
		t.Errorf("suggested_tx_pattern=%d steps, want 1", len(p.SuggestedTxPattern))
	}
	if provider.req.CohortSummary.EntityCount != 12 || provider.req.CohortSummary.TxCount != 250 {
		t.Errorf("provider saw cohort summary %+v, want counts from the stored manifest", provider.req.CohortSummary)
	}

	// The proposal survives a round trip through the repository.
	got, err := scenario.NewRepository(pool).Get(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("Get persisted proposal: %v", err)
	}
	if got.ID != p.ID || got.Description != p.Description || got.GeminiResponse == "" {
		t.Errorf("persisted proposal differs: %+v", got)
	}

	// And the detail endpoint serves it.
	detail := call(router, http.MethodGet, "/api/v1/scenarios/"+p.ID.String(), "")
	if detail.Code != http.StatusOK {
		t.Fatalf("detail: got %d, want 200", detail.Code)
	}
}

func TestScenarioHandler_CreateRequiresLockedCohort(t *testing.T) {
	pool := scenarioAPITestPool(t)
	unlocked := seedScenarioCohort(t, pool, false)

	h := NewScenarioHandler(scenario.NewRepository(pool), bank.NewRepository(pool), nil,
		&stubGeminiProvider{result: acceptedResult()})

	rec := call(scenarioRouter(h), http.MethodPost, "/api/v1/scenarios",
		`{"cohort_id":"`+unlocked.String()+`"}`)
	if rec.Code != http.StatusConflict {
		t.Errorf("unlocked cohort: got %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
}

func TestScenarioHandler_CreateRejectsMissingCohortAndProviderFailures(t *testing.T) {
	pool := scenarioAPITestPool(t)
	cohortID := seedScenarioCohort(t, pool, true)
	repo := scenario.NewRepository(pool)
	bankRepo := bank.NewRepository(pool)

	// Missing cohort_id.
	h := NewScenarioHandler(repo, bankRepo, nil, &stubGeminiProvider{result: acceptedResult()})
	rec := call(scenarioRouter(h), http.MethodPost, "/api/v1/scenarios", `{}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("missing cohort: got %d, want 422", rec.Code)
	}

	// Provider failure is a bad gateway, not a 500.
	h = NewScenarioHandler(repo, bankRepo, nil,
		&stubGeminiProvider{err: errors.New("upstream unavailable")})
	rec = call(scenarioRouter(h), http.MethodPost, "/api/v1/scenarios",
		`{"cohort_id":"`+cohortID.String()+`"}`)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("provider error: got %d, want 502", rec.Code)
	}

	// No candidate survived validation.
	h = NewScenarioHandler(repo, bankRepo, nil, &stubGeminiProvider{})
	rec = call(scenarioRouter(h), http.MethodPost, "/api/v1/scenarios",
		`{"cohort_id":"`+cohortID.String()+`"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("no accepted candidates: got %d, want 422", rec.Code)
	}
}
