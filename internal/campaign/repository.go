// Package campaign — repository.go
package campaign

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

// Repository provides campaign persistence.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository creates a Repository backed by the given pool.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// Create inserts a campaign. The caller must set ID, Name and Budget.
func (r *Repository) Create(ctx context.Context, c Campaign) (Campaign, error) {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	if c.Status == "" {
		c.Status = StatusActive
	}
	cfgJSON, err := marshalConfig(c.ControlConfig)
	if err != nil {
		return Campaign{}, err
	}
	now := time.Now().UTC()
	err = r.pool.QueryRow(ctx, `
		INSERT INTO campaign
		  (id, name, description, goal, cohort_id, budget, experiments_used,
		   status, control_config, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		RETURNING id, name, description, goal, cohort_id, budget,
		          experiments_used, status, control_config, created_at, updated_at`,
		c.ID, c.Name, c.Description, c.Goal, c.CohortID, c.Budget,
		c.ExperimentsUsed, string(c.Status), cfgJSON, now, now,
	).Scan(
		&c.ID, &c.Name, &c.Description, &c.Goal, &c.CohortID, &c.Budget,
		&c.ExperimentsUsed, &c.Status, &cfgJSON, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return Campaign{}, fmt.Errorf("campaign: create: %w", err)
	}
	if err := unmarshalConfig(cfgJSON, &c.ControlConfig); err != nil {
		return Campaign{}, err
	}
	return c, nil
}

// Get retrieves one campaign by ID.
func (r *Repository) Get(ctx context.Context, id uuid.UUID) (Campaign, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, name, description, goal, cohort_id, budget,
		       experiments_used, status, control_config, created_at, updated_at
		FROM campaign WHERE id = $1`, id)
	c, err := scanCampaign(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Campaign{}, ErrNotFound
		}
		return Campaign{}, err
	}
	return c, nil
}

// List returns campaigns, newest first.
func (r *Repository) List(ctx context.Context) ([]Campaign, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, name, description, goal, cohort_id, budget,
		       experiments_used, status, control_config, created_at, updated_at
		FROM campaign ORDER BY created_at DESC, id`)
	if err != nil {
		return nil, fmt.Errorf("campaign: list: %w", err)
	}
	defer rows.Close()

	out := make([]Campaign, 0)
	for rows.Next() {
		c, err := scanCampaign(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Update applies non-nil fields and returns the updated campaign.
func (r *Repository) Update(ctx context.Context, id uuid.UUID, description *string, budget *int, status *Status) (Campaign, error) {
	cur, err := r.Get(ctx, id)
	if err != nil {
		return Campaign{}, err
	}
	if description != nil {
		cur.Description = *description
	}
	if budget != nil {
		cur.Budget = *budget
	}
	if status != nil {
		cur.Status = *status
	}
	_, err = r.pool.Exec(ctx, `
		UPDATE campaign
		SET description = $2, budget = $3, status = $4, updated_at = NOW()
		WHERE id = $1`,
		id, cur.Description, cur.Budget, string(cur.Status))
	if err != nil {
		return Campaign{}, fmt.Errorf("campaign: update: %w", err)
	}
	return r.Get(ctx, id)
}

// IncrementUsed records one completed experiment against the campaign budget.
// It refuses to exceed the budget, so a concurrent request cannot overrun it.
func (r *Repository) IncrementUsed(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE campaign
		SET experiments_used = experiments_used + 1, updated_at = NOW()
		WHERE id = $1 AND status = 'active' AND experiments_used < budget`, id)
	if err != nil {
		return fmt.Errorf("campaign: increment used: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrBudgetExhausted
	}
	return nil
}

// CanRun reports whether the campaign can accept another experiment.
func (r *Repository) CanRun(ctx context.Context, id uuid.UUID) error {
	var status Status
	var used, budget int
	err := r.pool.QueryRow(ctx, `
		SELECT status, experiments_used, budget FROM campaign WHERE id = $1`, id).
		Scan(&status, &used, &budget)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("campaign: can run: %w", err)
	}
	if status != StatusActive {
		return ErrNotActive
	}
	if used >= budget {
		return ErrBudgetExhausted
	}
	return nil
}

// CountExperiments returns how many pairs are linked to the campaign.
func (r *Repository) CountExperiments(ctx context.Context, id uuid.UUID) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM experiment_pair WHERE campaign_id = $1`, id).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("campaign: count experiments: %w", err)
	}
	return n, nil
}

func scanCampaign(row pgx.Row) (Campaign, error) {
	var c Campaign
	var cfgJSON []byte
	err := row.Scan(
		&c.ID, &c.Name, &c.Description, &c.Goal, &c.CohortID, &c.Budget,
		&c.ExperimentsUsed, &c.Status, &cfgJSON, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return Campaign{}, err
	}
	if err := unmarshalConfig(cfgJSON, &c.ControlConfig); err != nil {
		return Campaign{}, err
	}
	return c, nil
}

func marshalConfig(c *aml.ControlConfig) ([]byte, error) {
	if c == nil {
		return nil, nil
	}
	b, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("campaign: marshal control config: %w", err)
	}
	return b, nil
}

func unmarshalConfig(raw []byte, dst **aml.ControlConfig) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var c aml.ControlConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return fmt.Errorf("campaign: unmarshal control config: %w", err)
	}
	*dst = &c
	return nil
}
