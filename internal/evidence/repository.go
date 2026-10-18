package evidence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository persists evidence bundles in PostgreSQL (JSONB).
// PRODUCT_SPEC item 7: bundles are stored and can optionally be exported to JSON.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository creates an evidence Repository.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// ErrBundleNotFound is returned when no bundle exists for an experiment.
var ErrBundleNotFound = errors.New("evidence bundle not found")

// Save upserts the immutable bundle for its experiment (Bundle.RunID).
// The checksum is computed if it is empty, so callers may pass a bundle with
// Checksum unset. Re-saving the same experiment overwrites (idempotent).
func (r *Repository) Save(ctx context.Context, b Bundle) error {
	if b.Checksum == "" {
		sum, err := ComputeChecksum(b)
		if err != nil {
			return err
		}
		b.Checksum = sum
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return fmt.Errorf("evidence: marshal bundle: %w", err)
	}

	_, err = r.pool.Exec(ctx, `
		INSERT INTO evidence_bundle (experiment_id, bundle, checksum)
		VALUES ($1, $2, $3)
		ON CONFLICT (experiment_id) DO UPDATE
		  SET bundle   = EXCLUDED.bundle,
		      checksum = EXCLUDED.checksum`,
		b.RunID, raw, b.Checksum)
	if err != nil {
		return fmt.Errorf("evidence: save bundle: %w", err)
	}
	return nil
}

// Get returns the persisted bundle for an experiment.
// Returns ErrBundleNotFound (wrapped) when none exists.
func (r *Repository) Get(ctx context.Context, experimentID uuid.UUID) (Bundle, error) {
	var (
		raw      []byte
		checksum string
	)
	err := r.pool.QueryRow(ctx,
		`SELECT bundle, checksum FROM evidence_bundle WHERE experiment_id = $1`,
		experimentID).Scan(&raw, &checksum)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Bundle{}, fmt.Errorf("%w for experiment %s", ErrBundleNotFound, experimentID)
		}
		return Bundle{}, fmt.Errorf("evidence: get bundle: %w", err)
	}
	var b Bundle
	if err := json.Unmarshal(raw, &b); err != nil {
		return Bundle{}, fmt.Errorf("evidence: unmarshal bundle: %w", err)
	}
	b.Checksum = checksum
	return b, nil
}
