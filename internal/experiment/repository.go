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
		   declared_change, mutation_type, mutation_spec, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		p.ID, p.Name, p.CohortID, p.ControlHash, p.ManifestHash,
		p.Seed, p.DeclaredChange, string(p.MutationType), specJSON, string(p.Status),
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
	return wrapErr("insert experiment_pair_run", err)
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
		       created_at, updated_at
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
		&p.CreatedAt, &p.UpdatedAt,
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
		runs = append(runs, rr)
	}
	return runs, rows.Err()
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

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
