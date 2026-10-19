// Package experiment — repository.go
//
// Repository persists and retrieves experiment pair records using pgx/v5.
// All writes are atomic (single transaction). Reads are single-connection queries.
package experiment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yourorg/financial-crime-red-team/internal/aml"
)

// Repository provides data access for the experiment layer.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository creates a Repository backed by the given pool.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// ─── Save ─────────────────────────────────────────────────────────────────────

// SavePairResult persists a complete ExecuteResult (pair + two runs + outcome)
// in a single database transaction. Idempotent on the pair ID.
func (r *Repository) SavePairResult(ctx context.Context, res *ExecuteResult) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("experiment repo: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) // nolint

	if err := insertPair(ctx, tx, res.Pair); err != nil {
		return err
	}
	if err := insertRun(ctx, tx, res.Baseline); err != nil {
		return err
	}
	if err := insertRun(ctx, tx, res.Variant); err != nil {
		return err
	}
	if err := insertOutcome(ctx, tx, res.Outcome); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func insertPair(ctx context.Context, tx pgx.Tx, p Pair) error {
	specJSON, err := json.Marshal(p.MutationSpec)
	if err != nil {
		return fmt.Errorf("marshal mutation_spec: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO experiment_pair
		  (id, name, cohort_id, control_hash, manifest_hash, seed,
		   declared_change, mutation_type, mutation_spec, scenario_proposal_id,
		   campaign_id, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		p.ID, p.Name, p.CohortID, p.ControlHash, p.ManifestHash,
		p.Seed, p.DeclaredChange, string(p.MutationType), specJSON,
		p.ScenarioProposalID, p.CampaignID, string(p.Status),
	)
	return wrapErr("insert experiment_pair", err)
}

func insertRun(ctx context.Context, tx pgx.Tx, r RunResult) error {
	alertsJSON, err := json.Marshal(r.RawAlerts)
	if err != nil {
		return fmt.Errorf("marshal raw_alerts: %w", err)
	}
	summaryJSON, err := json.Marshal(r.AlertSummary)
	if err != nil {
		return fmt.Errorf("marshal alert_summary: %w", err)
	}
	var completedAt *time.Time
	if !r.CompletedAt.IsZero() {
		completedAt = &r.CompletedAt
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO experiment_pair_run
		  (id, pair_id, run_type, control_hash, manifest_hash,
		   alert_count, alert_summary, raw_alerts, started_at, completed_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		r.ID, r.PairID, string(r.RunType), r.ControlHash, r.ManifestHash,
		r.AlertCount, summaryJSON, alertsJSON, r.StartedAt, completedAt,
	)
	if err != nil {
		return wrapErr("insert experiment_pair_run", err)
	}

	for _, oc := range r.Outcomes {
		if _, err := tx.Exec(ctx, `
			INSERT INTO rule_outcome
			  (id, run_id, pair_id, rule_id, rule_version, status, reason_code,
			   event_ids, account_ids, entity_ids, amount_cents, currency, detail)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
			uuid.New(), r.ID, r.PairID, string(oc.RuleID), oc.RuleVersion,
			string(oc.Status), string(oc.ReasonCode),
			nonNilIDs(oc.EventIDs), nonNilIDs(oc.AccountIDs), nonNilIDs(oc.EntityIDs),
			oc.AmountCents, oc.Currency, oc.Detail,
		); err != nil {
			return wrapErr("insert rule_outcome", err)
		}
	}
	return nil
}

// nonNilIDs ensures pgx receives an empty array rather than NULL for UUID[].
func nonNilIDs(ids []uuid.UUID) []uuid.UUID {
	if ids == nil {
		return []uuid.UUID{}
	}
	return ids
}

func insertOutcome(ctx context.Context, tx pgx.Tx, o Outcome) error {
	newRuleStrs := ruleIDsToStrings(o.NewAlertRuleIDs)
	droppedRuleStrs := ruleIDsToStrings(o.DroppedAlertRuleIDs)
	_, err := tx.Exec(ctx, `
		INSERT INTO experiment_outcome
		  (id, pair_id, baseline_run_id, variant_run_id,
		   evasion_delta, new_alert_rule_ids, dropped_alert_rule_ids, outcome_unchanged)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		o.ID, o.PairID, o.BaselineRunID, o.VariantRunID,
		o.EvasionDelta, newRuleStrs, droppedRuleStrs, o.OutcomeUnchanged,
	)
	return wrapErr("insert experiment_outcome", err)
}

// ─── Read ─────────────────────────────────────────────────────────────────────

// GetPair retrieves a pair record by ID.
func (r *Repository) GetPair(ctx context.Context, id uuid.UUID) (Pair, error) {
	const q = `
		SELECT id, name, cohort_id, control_hash, manifest_hash, seed,
		       declared_change, mutation_type, mutation_spec, status, error_msg,
		       scenario_proposal_id, campaign_id, created_at, updated_at
		FROM experiment_pair WHERE id = $1`
	row := r.pool.QueryRow(ctx, q, id)
	return scanPair(row)
}

func scanPair(row pgx.Row) (Pair, error) {
	var p Pair
	var specJSON []byte
	var errMsg *string
	err := row.Scan(
		&p.ID, &p.Name, &p.CohortID, &p.ControlHash, &p.ManifestHash, &p.Seed,
		&p.DeclaredChange, &p.MutationType, &specJSON, &p.Status, &errMsg,
		&p.ScenarioProposalID, &p.CampaignID, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Pair{}, fmt.Errorf("experiment pair not found: %w", err)
		}
		return Pair{}, fmt.Errorf("scan pair: %w", err)
	}
	if errMsg != nil {
		p.ErrorMsg = *errMsg
	}
	if err := json.Unmarshal(specJSON, &p.MutationSpec); err != nil {
		return Pair{}, fmt.Errorf("unmarshal mutation_spec: %w", err)
	}
	return p, nil
}

// GetOutcome retrieves the outcome for a pair by pair ID.
func (r *Repository) GetOutcome(ctx context.Context, pairID uuid.UUID) (Outcome, error) {
	const q = `
		SELECT id, pair_id, baseline_run_id, variant_run_id,
		       evasion_delta, new_alert_rule_ids, dropped_alert_rule_ids,
		       outcome_unchanged, created_at
		FROM experiment_outcome WHERE pair_id = $1`
	row := r.pool.QueryRow(ctx, q, pairID)

	var o Outcome
	var newRuleStrs, droppedRuleStrs []string
	err := row.Scan(
		&o.ID, &o.PairID, &o.BaselineRunID, &o.VariantRunID,
		&o.EvasionDelta, &newRuleStrs, &droppedRuleStrs,
		&o.OutcomeUnchanged, &o.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Outcome{}, fmt.Errorf("outcome not found for pair %s: %w", pairID, err)
		}
		return Outcome{}, fmt.Errorf("scan outcome: %w", err)
	}
	o.NewAlertRuleIDs = stringsToRuleIDs(newRuleStrs)
	o.DroppedAlertRuleIDs = stringsToRuleIDs(droppedRuleStrs)
	return o, nil
}

// ListRunsForPair retrieves both runs for a pair, ordered by run_type.
func (r *Repository) ListRunsForPair(ctx context.Context, pairID uuid.UUID) ([]RunResult, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, pair_id, run_type, control_hash, manifest_hash,
		       alert_count, alert_summary, raw_alerts, started_at, completed_at
		FROM experiment_pair_run
		WHERE pair_id = $1
		ORDER BY run_type`, pairID)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer rows.Close()

	var runs []RunResult
	for rows.Next() {
		var rr RunResult
		var alertsJSON, summaryJSON []byte
		var completedAt *time.Time
		if err := rows.Scan(
			&rr.ID, &rr.PairID, &rr.RunType, &rr.ControlHash, &rr.ManifestHash,
			&rr.AlertCount, &summaryJSON, &alertsJSON, &rr.StartedAt, &completedAt,
		); err != nil {
			return nil, fmt.Errorf("scan run: %w", err)
		}
		if completedAt != nil {
			rr.CompletedAt = *completedAt
		}
		if err := json.Unmarshal(alertsJSON, &rr.RawAlerts); err != nil {
			return nil, fmt.Errorf("unmarshal raw_alerts: %w", err)
		}
		if err := json.Unmarshal(summaryJSON, &rr.AlertSummary); err != nil {
			return nil, fmt.Errorf("unmarshal alert_summary: %w", err)
		}
		outcomes, err := r.loadOutcomes(ctx, rr.ID)
		if err != nil {
			return nil, err
		}
		rr.Outcomes = outcomes
		runs = append(runs, rr)
	}
	return runs, rows.Err()
}

// loadOutcomes reads the five-status outcomes persisted for one run, so the
// experiment detail endpoint returns the same evidence the coverage maps were
// built from.
func (r *Repository) loadOutcomes(ctx context.Context, runID uuid.UUID) ([]aml.RuleOutcome, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT rule_id, rule_version, status, reason_code,
		       event_ids, account_ids, entity_ids,
		       amount_cents, currency, detail
		FROM rule_outcome
		WHERE run_id = $1
		ORDER BY rule_id`, runID)
	if err != nil {
		return nil, fmt.Errorf("experiment repo: load outcomes: %w", err)
	}
	defer rows.Close()

	var outcomes []aml.RuleOutcome
	for rows.Next() {
		var oc aml.RuleOutcome
		if err := rows.Scan(
			&oc.RuleID, &oc.RuleVersion, &oc.Status, &oc.ReasonCode,
			&oc.EventIDs, &oc.AccountIDs, &oc.EntityIDs,
			&oc.AmountCents, &oc.Currency, &oc.Detail,
		); err != nil {
			return nil, fmt.Errorf("experiment repo: scan outcome: %w", err)
		}
		outcomes = append(outcomes, oc)
	}
	return outcomes, rows.Err()
}

// ─── Review decisions ─────────────────────────────────────────────────────────

// SaveReview records a reviewer decision. A pair can be reviewed once; a second
// attempt returns ErrAlreadyReviewed rather than silently overwriting history.
func (r *Repository) SaveReview(ctx context.Context, d ReviewDecision) (ReviewDecision, error) {
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	err := r.pool.QueryRow(ctx, `
		INSERT INTO review_decision (id, pair_id, reviewer, decision, rationale)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, pair_id, reviewer, decision, rationale, created_at`,
		d.ID, d.PairID, d.Reviewer, string(d.Decision), d.Rationale,
	).Scan(&d.ID, &d.PairID, &d.Reviewer, &d.Decision, &d.Rationale, &d.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ReviewDecision{}, ErrAlreadyReviewed
		}
		return ReviewDecision{}, fmt.Errorf("experiment repo: save review: %w", err)
	}
	return d, nil
}

// GetReview returns the decision for a pair, or a wrapped pgx.ErrNoRows.
func (r *Repository) GetReview(ctx context.Context, pairID uuid.UUID) (ReviewDecision, error) {
	var d ReviewDecision
	err := r.pool.QueryRow(ctx, `
		SELECT id, pair_id, reviewer, decision, rationale, created_at
		FROM review_decision WHERE pair_id = $1`, pairID).
		Scan(&d.ID, &d.PairID, &d.Reviewer, &d.Decision, &d.Rationale, &d.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ReviewDecision{}, fmt.Errorf("review not found for pair %s: %w", pairID, err)
		}
		return ReviewDecision{}, fmt.Errorf("experiment repo: get review: %w", err)
	}
	return d, nil
}

// ListReviewQueue returns completed pairs that have no review decision yet,
// oldest first (the longest-waiting experiment is reviewed first).
func (r *Repository) ListReviewQueue(ctx context.Context, limit int) ([]PairSummary, error) {
	if limit <= 0 {
		limit = 100
	}
	summaries, err := r.ListPairSummaries(ctx, 1000)
	if err != nil {
		return nil, err
	}

	rows, err := r.pool.Query(ctx, `SELECT pair_id FROM review_decision`)
	if err != nil {
		return nil, fmt.Errorf("experiment repo: list reviewed pairs: %w", err)
	}
	defer rows.Close()
	reviewed := make(map[uuid.UUID]bool)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("experiment repo: scan reviewed pair: %w", err)
		}
		reviewed[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]PairSummary, 0, limit)
	// ListPairSummaries is newest-first; walk backwards so the queue is oldest-first.
	for i := len(summaries) - 1; i >= 0 && len(out) < limit; i-- {
		s := summaries[i]
		if s.Pair.Status != StatusCompleted || reviewed[s.Pair.ID] {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// PairSummary is a pair together with its outcome (nil until the pair
// completes). Used by the list and overview endpoints.
type PairSummary struct {
	Pair    Pair
	Outcome *Outcome
}

// ListPairSummaries returns up to limit pairs (newest first) with their
// outcomes joined via LEFT JOIN, so in-flight pairs are included too.
func (r *Repository) ListPairSummaries(ctx context.Context, limit int) ([]PairSummary, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := r.pool.Query(ctx, `
		SELECT p.id, p.name, p.cohort_id, p.control_hash, p.manifest_hash, p.seed,
		       p.declared_change, p.mutation_type, p.mutation_spec, p.status, p.error_msg,
		       p.created_at, p.updated_at,
		       o.id, o.pair_id, o.baseline_run_id, o.variant_run_id, o.evasion_delta,
		       o.new_alert_rule_ids, o.dropped_alert_rule_ids, o.outcome_unchanged, o.created_at
		FROM experiment_pair p
		LEFT JOIN experiment_outcome o ON o.pair_id = p.id
		ORDER BY p.created_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list pair summaries: %w", err)
	}
	defer rows.Close()

	var out []PairSummary
	for rows.Next() {
		var s PairSummary
		var specJSON []byte
		var errMsg *string
		var (
			oID, oPairID, oBaseline, oVariant *uuid.UUID
			oDelta                            *int
			oNew, oDropped                    []string
			oUnchanged                        *bool
			oCreated                          *time.Time
		)
		if err := rows.Scan(
			&s.Pair.ID, &s.Pair.Name, &s.Pair.CohortID, &s.Pair.ControlHash,
			&s.Pair.ManifestHash, &s.Pair.Seed, &s.Pair.DeclaredChange,
			&s.Pair.MutationType, &specJSON, &s.Pair.Status, &errMsg,
			&s.Pair.CreatedAt, &s.Pair.UpdatedAt,
			&oID, &oPairID, &oBaseline, &oVariant, &oDelta,
			&oNew, &oDropped, &oUnchanged, &oCreated,
		); err != nil {
			return nil, fmt.Errorf("scan pair summary: %w", err)
		}
		if errMsg != nil {
			s.Pair.ErrorMsg = *errMsg
		}
		if err := json.Unmarshal(specJSON, &s.Pair.MutationSpec); err != nil {
			return nil, fmt.Errorf("unmarshal mutation_spec: %w", err)
		}
		if oID != nil {
			s.Outcome = &Outcome{
				ID:                  *oID,
				PairID:              *oPairID,
				BaselineRunID:       *oBaseline,
				VariantRunID:        *oVariant,
				EvasionDelta:        *oDelta,
				NewAlertRuleIDs:     stringsToRuleIDs(oNew),
				DroppedAlertRuleIDs: stringsToRuleIDs(oDropped),
				OutcomeUnchanged:    *oUnchanged,
				CreatedAt:           *oCreated,
			}
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// CompletedPair bundles a pair with its computed outcome for regression packing.
type CompletedPair struct {
	Pair    Pair
	Outcome Outcome
}

// ListCompletedPairsWithOutcomes returns all completed pairs joined with their
// outcomes, ordered by creation time (oldest first). Pairs without an outcome
// row are skipped. It is used to build a Regression Test Pack.
func (r *Repository) ListCompletedPairsWithOutcomes(ctx context.Context, limit int) ([]CompletedPair, error) {
	if limit <= 0 {
		limit = 1000
	}
	rows, err := r.pool.Query(ctx, `
		SELECT p.id, p.name, p.cohort_id, p.control_hash, p.manifest_hash, p.seed,
		       p.declared_change, p.mutation_type, p.mutation_spec, p.status, p.error_msg,
		       p.created_at, p.updated_at,
		       o.id, o.pair_id, o.baseline_run_id, o.variant_run_id,
		       o.evasion_delta, o.new_alert_rule_ids, o.dropped_alert_rule_ids,
		       o.outcome_unchanged, o.created_at
		FROM experiment_pair p
		JOIN experiment_outcome o ON o.pair_id = p.id
		WHERE p.status = 'completed'
		ORDER BY p.created_at ASC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list completed pairs: %w", err)
	}
	defer rows.Close()

	var out []CompletedPair
	for rows.Next() {
		var cp CompletedPair
		var specJSON []byte
		var errMsg *string
		var newRuleStrs, droppedRuleStrs []string

		if err := rows.Scan(
			&cp.Pair.ID, &cp.Pair.Name, &cp.Pair.CohortID, &cp.Pair.ControlHash,
			&cp.Pair.ManifestHash, &cp.Pair.Seed, &cp.Pair.DeclaredChange,
			&cp.Pair.MutationType, &specJSON, &cp.Pair.Status, &errMsg,
			&cp.Pair.CreatedAt, &cp.Pair.UpdatedAt,
			&cp.Outcome.ID, &cp.Outcome.PairID, &cp.Outcome.BaselineRunID,
			&cp.Outcome.VariantRunID, &cp.Outcome.EvasionDelta, &newRuleStrs,
			&droppedRuleStrs, &cp.Outcome.OutcomeUnchanged, &cp.Outcome.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan completed pair: %w", err)
		}
		if errMsg != nil {
			cp.Pair.ErrorMsg = *errMsg
		}
		if err := json.Unmarshal(specJSON, &cp.Pair.MutationSpec); err != nil {
			return nil, fmt.Errorf("unmarshal mutation_spec: %w", err)
		}
		cp.Outcome.NewAlertRuleIDs = stringsToRuleIDs(newRuleStrs)
		cp.Outcome.DroppedAlertRuleIDs = stringsToRuleIDs(droppedRuleStrs)
		out = append(out, cp)
	}
	return out, rows.Err()
}

func ruleIDsToStrings(ids []aml.RuleID) []string {
	s := make([]string, len(ids))
	for i, id := range ids {
		s[i] = string(id)
	}
	return s
}

func stringsToRuleIDs(strs []string) []aml.RuleID {
	ids := make([]aml.RuleID, len(strs))
	for i, s := range strs {
		ids[i] = aml.RuleID(s)
	}
	return ids
}

func wrapErr(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("experiment repo %s: %w", op, err)
}
