// Package bank — repository.go
//
// Repository persists and retrieves bank domain objects using pgx/v5.
//
// All methods accept a context.Context so callers can propagate deadlines
// and cancellation. All writes check the cohort lock before executing.
//
// Monetary storage: AmountCents (int64) is stored in the 'amount' column as
// NUMERIC(18,4) with an implicit scaling factor of 1/100 (i.e. the DB stores
// the decimal representation of cents/100). BalanceCents likewise.
// This preserves exact arithmetic in the DB while avoiding pgtype.Numeric
// complexity in application code.
//
// Note: this package intentionally has no HTTP or business logic.
package bank

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DB is the interface satisfied by both *pgxpool.Pool and pgx.Tx, allowing
// repository methods to participate in transactions.
// any is used here intentionally: pgx's Rows/Row interface uses it.
type DB interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (interface{ RowsAffected() int64 }, error)
}

// Repository provides data access for the bank domain.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository creates a new Repository backed by the given connection pool.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// ─── Cohort ──────────────────────────────────────────────────────────────────

// CreateCohort inserts a new cohort row and returns it with DB-assigned timestamps.
func (r *Repository) CreateCohort(ctx context.Context, c Cohort) (Cohort, error) {
	predJSON, err := json.Marshal(c.Predicate)
	if err != nil {
		return Cohort{}, fmt.Errorf("marshal predicate: %w", err)
	}
	manifestJSON, err := json.Marshal(c.Manifest)
	if err != nil {
		return Cohort{}, fmt.Errorf("marshal manifest: %w", err)
	}

	const q = `
		INSERT INTO cohort (id, name, predicate, seed, generator_version, manifest,
		                    window_start, window_end)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, name, predicate, seed, generator_version, manifest,
		          window_start, window_end, locked_at, created_at, updated_at`

	row := r.pool.QueryRow(ctx, q,
		c.ID, c.Name, predJSON, c.Seed,
		c.GeneratorVersion, manifestJSON,
		nullTime(c.WindowStart), nullTime(c.WindowEnd),
	)
	return scanCohort(row)
}

// GetCohort retrieves a cohort by ID.
func (r *Repository) GetCohort(ctx context.Context, id uuid.UUID) (Cohort, error) {
	const q = `
		SELECT id, name, predicate, seed, generator_version, manifest,
		       window_start, window_end, locked_at, created_at, updated_at
		FROM cohort WHERE id = $1`
	row := r.pool.QueryRow(ctx, q, id)
	return scanCohort(row)
}

// ListCohorts returns all cohorts ordered by created_at DESC.
func (r *Repository) ListCohorts(ctx context.Context) ([]Cohort, error) {
	const q = `
		SELECT id, name, predicate, seed, generator_version, manifest,
		       window_start, window_end, locked_at, created_at, updated_at
		FROM cohort
		ORDER BY created_at DESC`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list cohorts: %w", err)
	}
	defer rows.Close()

	var cohorts []Cohort
	for rows.Next() {
		var c Cohort
		var predJSON, manifestJSON []byte
		var windowStart, windowEnd, lockedAt *time.Time
		if err := rows.Scan(
			&c.ID, &c.Name, &predJSON, &c.Seed, &c.GeneratorVersion, &manifestJSON,
			&windowStart, &windowEnd, &lockedAt, &c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan cohort row: %w", err)
		}
		if err := json.Unmarshal(predJSON, &c.Predicate); err != nil {
			return nil, fmt.Errorf("unmarshal predicate: %w", err)
		}
		if len(manifestJSON) > 0 {
			if err := json.Unmarshal(manifestJSON, &c.Manifest); err != nil {
				return nil, fmt.Errorf("unmarshal manifest: %w", err)
			}
		}
		c.WindowStart = windowStart
		c.WindowEnd = windowEnd
		c.LockedAt = lockedAt
		cohorts = append(cohorts, c)
	}
	return cohorts, rows.Err()
}

// LockCohort sets locked_at to now, making the cohort immutable.
// Returns ErrCohortLocked if already locked.
func (r *Repository) LockCohort(ctx context.Context, id uuid.UUID) (Cohort, error) {
	// Check current state first.
	c, err := r.GetCohort(ctx, id)
	if err != nil {
		return Cohort{}, err
	}
	if c.LockedAt != nil {
		return Cohort{}, ErrCohortLocked
	}

	const q = `
		UPDATE cohort SET locked_at = NOW()
		WHERE id = $1
		RETURNING id, name, predicate, seed, generator_version, manifest,
		          window_start, window_end, locked_at, created_at, updated_at`
	row := r.pool.QueryRow(ctx, q, id)
	return scanCohort(row)
}

// UpdateManifest updates the manifest and generator_version fields of a cohort.
// Used after generation completes to record final counts.
// Returns ErrCohortLocked if the cohort is already locked.
func (r *Repository) UpdateManifest(ctx context.Context, id uuid.UUID, m CohortManifest, version string) error {
	c, err := r.GetCohort(ctx, id)
	if err != nil {
		return err
	}
	if c.LockedAt != nil {
		return ErrCohortLocked
	}
	mJSON, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	_, err = r.pool.Exec(ctx,
		`UPDATE cohort SET manifest = $1, generator_version = $2,
		                   window_start = $3, window_end = $4
		 WHERE id = $5`,
		mJSON, version, m.WindowStart, m.WindowEnd, id)
	return err
}

// scanCohort scans a single cohort row from a pgx.Row.
func scanCohort(row pgx.Row) (Cohort, error) {
	var c Cohort
	var predJSON, manifestJSON []byte
	var windowStart, windowEnd, lockedAt *time.Time
	err := row.Scan(
		&c.ID, &c.Name, &predJSON, &c.Seed, &c.GeneratorVersion, &manifestJSON,
		&windowStart, &windowEnd, &lockedAt, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Cohort{}, fmt.Errorf("cohort not found: %w", err)
		}
		return Cohort{}, fmt.Errorf("scan cohort: %w", err)
	}
	if err := json.Unmarshal(predJSON, &c.Predicate); err != nil {
		return Cohort{}, fmt.Errorf("unmarshal predicate: %w", err)
	}
	if len(manifestJSON) > 0 {
		if err := json.Unmarshal(manifestJSON, &c.Manifest); err != nil {
			return Cohort{}, fmt.Errorf("unmarshal manifest: %w", err)
		}
	}
	c.WindowStart = windowStart
	c.WindowEnd = windowEnd
	c.LockedAt = lockedAt
	return c, nil
}

// ─── SaveGenerated ───────────────────────────────────────────────────────────

// SaveGenerated persists a complete GenerateResult in a single transaction.
// It checks the cohort lock, then bulk-inserts entities, accounts, and
// transactions. On success it updates the cohort manifest and locks the cohort.
//
// This is an all-or-nothing operation: if any insert fails, all changes are
// rolled back and the cohort remains in its pre-generation state.
func (r *Repository) SaveGenerated(ctx context.Context, result *GenerateResult) error {
	// Check lock before opening the transaction.
	c, err := r.GetCohort(ctx, result.Manifest.CohortID)
	if err != nil {
		return err
	}
	if c.LockedAt != nil {
		return ErrCohortLocked
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) // nolint: errcheck — rollback on any path that doesn't commit

	// Insert entities.
	if err := insertEntities(ctx, tx, result.Entities); err != nil {
		return err
	}
	// Insert accounts.
	if err := insertAccounts(ctx, tx, result.Accounts); err != nil {
		return err
	}
	// Insert transactions.
	if err := insertTransactions(ctx, tx, result.Transactions); err != nil {
		return err
	}
	// Update manifest and generator version.
	mJSON, err := json.Marshal(result.Manifest)
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	_, err = tx.Exec(ctx,
		`UPDATE cohort SET manifest = $1, generator_version = $2,
		                   window_start = $3, window_end = $4
		 WHERE id = $5`,
		mJSON, result.Manifest.GeneratorVersion,
		result.Manifest.WindowStart, result.Manifest.WindowEnd,
		result.Manifest.CohortID,
	)
	if err != nil {
		return fmt.Errorf("update cohort manifest: %w", err)
	}
	// Lock the cohort.
	_, err = tx.Exec(ctx,
		`UPDATE cohort SET locked_at = NOW() WHERE id = $1`,
		result.Manifest.CohortID,
	)
	if err != nil {
		return fmt.Errorf("lock cohort: %w", err)
	}

	return tx.Commit(ctx)
}

func insertEntities(ctx context.Context, tx pgx.Tx, entities []Entity) error {
	for _, e := range entities {
		attrsJSON, err := json.Marshal(e.Attributes)
		if err != nil {
			return fmt.Errorf("marshal entity attributes: %w", err)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO bank_entity
			  (id, cohort_id, entity_type, name, country_code, risk_score, attributes, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			e.ID, e.CohortID, string(e.EntityType), e.Name,
			e.CountryCode, e.RiskScore, attrsJSON, e.CreatedAt,
		)
		if err != nil {
			return fmt.Errorf("insert entity %s: %w", e.ID, err)
		}
	}
	return nil
}

func insertAccounts(ctx context.Context, tx pgx.Tx, accounts []Account) error {
	for _, a := range accounts {
		attrsJSON, err := json.Marshal(a.Attributes)
		if err != nil {
			return fmt.Errorf("marshal account attributes: %w", err)
		}
		// balance_cents is BIGINT — pass int64 directly, not a decimal string.
		_, err = tx.Exec(ctx, `
			INSERT INTO bank_account
			  (id, cohort_id, entity_id, account_type, currency,
			   balance_cents, opened_at, attributes, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			a.ID, a.CohortID, a.EntityID, string(a.AccountType), a.Currency,
			a.BalanceCents, a.OpenedAt, attrsJSON, a.CreatedAt,
		)
		if err != nil {
			return fmt.Errorf("insert account %s: %w", a.ID, err)
		}
	}
	return nil
}

func insertTransactions(ctx context.Context, tx pgx.Tx, txns []Transaction) error {
	for _, t := range txns {
		attrsJSON, err := json.Marshal(t.Attributes)
		if err != nil {
			return fmt.Errorf("marshal tx attributes: %w", err)
		}
		amountDecimal := centsToDecimalStr(t.AmountCents)
		_, err = tx.Exec(ctx, `
			INSERT INTO bank_transaction
			  (id, cohort_id, from_account_id, to_account_id,
			   amount, currency, tx_type, tx_timestamp, reference, attributes, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
			t.ID, t.CohortID, t.FromAccountID, t.ToAccountID,
			amountDecimal, t.Currency, string(t.TxType),
			t.TxTimestamp, nullString(t.Reference), attrsJSON, t.CreatedAt,
		)
		if err != nil {
			return fmt.Errorf("insert tx %s: %w", t.ID, err)
		}
	}
	return nil
}

// ─── Batch Selection ─────────────────────────────────────────────────────────

// SelectBatch returns a BatchSelection for the given cohort, filtering
// transactions to those within [windowStart, windowEnd).
// The cohort must be locked; returns ErrCohortLocked if it is not.
func (r *Repository) SelectBatch(
	ctx context.Context,
	cohortID uuid.UUID,
	windowStart, windowEnd time.Time,
) (*BatchSelection, error) {
	c, err := r.GetCohort(ctx, cohortID)
	if err != nil {
		return nil, err
	}
	// Batch selection is a read-only operation, but we still require a locked
	// cohort to guarantee the data set is stable.
	if c.LockedAt == nil {
		return nil, fmt.Errorf("cohort must be locked before batch selection")
	}

	entities, err := r.ListEntities(ctx, cohortID)
	if err != nil {
		return nil, err
	}
	accounts, err := r.ListAccounts(ctx, cohortID)
	if err != nil {
		return nil, err
	}
	txns, err := r.ListTransactionsInWindow(ctx, cohortID, windowStart, windowEnd)
	if err != nil {
		return nil, err
	}

	return &BatchSelection{
		CohortID:     cohortID,
		WindowStart:  windowStart,
		WindowEnd:    windowEnd,
		Manifest:     c.Manifest,
		Entities:     entities,
		Accounts:     accounts,
		Transactions: txns,
	}, nil
}

// ─── Entity queries ──────────────────────────────────────────────────────────

// ListEntities returns all entities for a cohort, ordered by created_at, id.
func (r *Repository) ListEntities(ctx context.Context, cohortID uuid.UUID) ([]Entity, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, cohort_id, entity_type, name, country_code, risk_score, attributes, created_at
		FROM bank_entity
		WHERE cohort_id = $1
		ORDER BY created_at, id`, cohortID)
	if err != nil {
		return nil, fmt.Errorf("list entities: %w", err)
	}
	defer rows.Close()

	var entities []Entity
	for rows.Next() {
		var e Entity
		var attrsJSON []byte
		if err := rows.Scan(
			&e.ID, &e.CohortID, &e.EntityType, &e.Name,
			&e.CountryCode, &e.RiskScore, &attrsJSON, &e.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan entity: %w", err)
		}
		if err := json.Unmarshal(attrsJSON, &e.Attributes); err != nil {
			return nil, fmt.Errorf("unmarshal entity attributes: %w", err)
		}
		entities = append(entities, e)
	}
	return entities, rows.Err()
}

// ─── Account queries ─────────────────────────────────────────────────────────

// ListAccounts returns all accounts for a cohort, ordered by created_at, id.
func (r *Repository) ListAccounts(ctx context.Context, cohortID uuid.UUID) ([]Account, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, cohort_id, entity_id, account_type, currency,
		       balance_cents, opened_at, attributes, created_at
		FROM bank_account
		WHERE cohort_id = $1
		ORDER BY created_at, id`, cohortID)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	defer rows.Close()

	var accounts []Account
	for rows.Next() {
		var a Account
		var attrsJSON []byte
		// balance_cents is BIGINT — scan directly into int64.
		if err := rows.Scan(
			&a.ID, &a.CohortID, &a.EntityID, &a.AccountType, &a.Currency,
			&a.BalanceCents, &a.OpenedAt, &attrsJSON, &a.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan account: %w", err)
		}
		if err := json.Unmarshal(attrsJSON, &a.Attributes); err != nil {
			return nil, fmt.Errorf("unmarshal account attributes: %w", err)
		}
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}

// ─── Transaction queries ──────────────────────────────────────────────────────

// ListTransactionsInWindow returns transactions for a cohort within [start, end).
// Results are ordered by tx_timestamp, id.
func (r *Repository) ListTransactionsInWindow(
	ctx context.Context,
	cohortID uuid.UUID,
	start, end time.Time,
) ([]Transaction, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, cohort_id, from_account_id, to_account_id,
		       amount, currency, tx_type, tx_timestamp, reference, attributes, created_at
		FROM bank_transaction
		WHERE cohort_id = $1
		  AND tx_timestamp >= $2
		  AND tx_timestamp < $3
		ORDER BY tx_timestamp, id`,
		cohortID, start, end)
	if err != nil {
		return nil, fmt.Errorf("list transactions: %w", err)
	}
	defer rows.Close()

	var txns []Transaction
	for rows.Next() {
		var t Transaction
		var attrsJSON []byte
		var amountStr string
		var ref *string
		if err := rows.Scan(
			&t.ID, &t.CohortID, &t.FromAccountID, &t.ToAccountID,
			&amountStr, &t.Currency, &t.TxType,
			&t.TxTimestamp, &ref, &attrsJSON, &t.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan transaction: %w", err)
		}
		t.AmountCents = decimalStrToCents(amountStr)
		if ref != nil {
			t.Reference = *ref
		}
		if err := json.Unmarshal(attrsJSON, &t.Attributes); err != nil {
			return nil, fmt.Errorf("unmarshal tx attributes: %w", err)
		}
		txns = append(txns, t)
	}
	return txns, rows.Err()
}

// ─── Monetary conversion helpers ─────────────────────────────────────────────

// centsToDecimalStr converts an integer cents value to a decimal string
// suitable for PostgreSQL NUMERIC columns.
// e.g. 12345 cents → "123.45"
func centsToDecimalStr(cents int64) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	return fmt.Sprintf("%s%d.%02d", sign, cents/100, cents%100)
}

// decimalStrToCents parses a PostgreSQL NUMERIC string back to integer cents.
// Handles formats: "123.45", "123", "123.4", "123.456" (truncated to 2dp).
func decimalStrToCents(s string) int64 {
	if s == "" {
		return 0
	}
	negative := false
	if len(s) > 0 && s[0] == '-' {
		negative = true
		s = s[1:]
	}
	// Find decimal point.
	dotIdx := -1
	for i, c := range s {
		if c == '.' {
			dotIdx = i
			break
		}
	}
	var intPart, fracPart string
	if dotIdx == -1 {
		intPart = s
		fracPart = "00"
	} else {
		intPart = s[:dotIdx]
		frac := s[dotIdx+1:]
		switch len(frac) {
		case 0:
			fracPart = "00"
		case 1:
			fracPart = frac + "0"
		default:
			fracPart = frac[:2] // truncate to 2dp
		}
	}
	var ip, fp int64
	fmt.Sscanf(intPart, "%d", &ip)
	fmt.Sscanf(fracPart, "%d", &fp)
	result := ip*100 + fp
	if negative {
		result = -result
	}
	return result
}

// ─── Null helpers ─────────────────────────────────────────────────────────────

// nullTime returns a *time.Time pointer, nil if t is zero.
func nullTime(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return *t
}

func nullString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
