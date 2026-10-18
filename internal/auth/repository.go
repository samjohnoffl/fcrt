package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Role values are constrained by the app_user CHECK constraint and mirror the
// api package's authorization roles.
const (
	RoleAnalyst = "analyst"
	RoleAdmin   = "admin"
)

// User is an application account. The password hash never leaves this package.
type User struct {
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	Disabled  bool      `json:"disabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ValidRole reports whether role is a permitted application role.
func ValidRole(role string) bool {
	return role == RoleAnalyst || role == RoleAdmin
}

// Sentinel errors.
var (
	ErrUserNotFound  = errors.New("auth: user not found")
	ErrUsernameTaken = errors.New("auth: username already exists")
	ErrInvalidRole   = errors.New("auth: invalid role")
)

// Repository is the PostgreSQL-backed user directory.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository creates a Repository.
func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

const userCols = `username, role, disabled, created_at, updated_at`

// scanUser works for both pgx.Row and pgx.Rows.
func scanUser(row pgx.Row) (User, error) {
	var u User
	err := row.Scan(&u.Username, &u.Role, &u.Disabled, &u.CreatedAt, &u.UpdatedAt)
	return u, err
}

// Authenticate verifies credentials. It returns (User{}, false, nil) for bad
// credentials and a non-nil error only for infrastructure failures.
func (r *Repository) Authenticate(ctx context.Context, username, password string) (User, bool, error) {
	var hash string
	var u User
	err := r.pool.QueryRow(ctx,
		`SELECT `+userCols+`, password_hash FROM app_user WHERE username = $1`, username,
	).Scan(&u.Username, &u.Role, &u.Disabled, &u.CreatedAt, &u.UpdatedAt, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		VerifyPassword(dummyHash, password) // equalise timing
		return User{}, false, nil
	}
	if err != nil {
		return User{}, false, fmt.Errorf("auth: lookup user: %w", err)
	}
	if u.Disabled || !VerifyPassword(hash, password) {
		return User{}, false, nil
	}
	return u, true, nil
}

// List returns all accounts ordered by username.
func (r *Repository) List(ctx context.Context) ([]User, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+userCols+` FROM app_user ORDER BY username`)
	if err != nil {
		return nil, fmt.Errorf("auth: list users: %w", err)
	}
	defer rows.Close()

	out := make([]User, 0)
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("auth: scan user: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// Get returns a single account.
func (r *Repository) Get(ctx context.Context, username string) (User, error) {
	u, err := scanUser(r.pool.QueryRow(ctx,
		`SELECT `+userCols+` FROM app_user WHERE username = $1`, username))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("auth: get user: %w", err)
	}
	return u, nil
}

// Create inserts a new account with the given plaintext password.
func (r *Repository) Create(ctx context.Context, username, password, role string) (User, error) {
	if !ValidRole(role) {
		return User{}, ErrInvalidRole
	}
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}
	u, err := scanUser(r.pool.QueryRow(ctx,
		`INSERT INTO app_user (username, password_hash, role) VALUES ($1, $2, $3)
		 RETURNING `+userCols, username, hash, role))
	if err != nil {
		if isUniqueViolation(err) {
			return User{}, ErrUsernameTaken
		}
		return User{}, fmt.Errorf("auth: create user: %w", err)
	}
	return u, nil
}

// Update applies a partial change. Nil fields are left unchanged; a non-nil
// password is re-hashed.
func (r *Repository) Update(ctx context.Context, username string, role, password *string, disabled *bool) (User, error) {
	if role != nil && !ValidRole(*role) {
		return User{}, ErrInvalidRole
	}
	var hash *string
	if password != nil {
		h, err := HashPassword(*password)
		if err != nil {
			return User{}, err
		}
		hash = &h
	}
	u, err := scanUser(r.pool.QueryRow(ctx,
		`UPDATE app_user SET
		    role          = COALESCE($2, role),
		    password_hash = COALESCE($3, password_hash),
		    disabled      = COALESCE($4, disabled),
		    updated_at    = now()
		  WHERE username = $1
		 RETURNING `+userCols, username, role, hash, disabled))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("auth: update user: %w", err)
	}
	return u, nil
}

// Delete removes an account.
func (r *Repository) Delete(ctx context.Context, username string) error {
	ct, err := r.pool.Exec(ctx, `DELETE FROM app_user WHERE username = $1`, username)
	if err != nil {
		return fmt.Errorf("auth: delete user: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

// EnsureUser inserts a user only if the username does not exist yet. Used to
// seed the built-in accounts on boot without clobbering admin edits.
//
// It deliberately bypasses the minimum-length policy so that the documented
// bootstrap credentials (e.g. admin/admin) work out of the box; the policy is
// enforced for accounts created through the API.
func (r *Repository) EnsureUser(ctx context.Context, username, password, role string) error {
	if !ValidRole(role) {
		return ErrInvalidRole
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx,
		`INSERT INTO app_user (username, password_hash, role) VALUES ($1, $2, $3)
		 ON CONFLICT (org_id, username) DO NOTHING`, username, hash, role)
	if err != nil {
		return fmt.Errorf("auth: seed user: %w", err)
	}
	return nil
}

// Count returns the number of accounts (used to detect an unseeded directory).
func (r *Repository) Count(ctx context.Context) (int, error) {
	var n int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM app_user`).Scan(&n); err != nil {
		return 0, fmt.Errorf("auth: count users: %w", err)
	}
	return n, nil
}

// isUniqueViolation detects a PostgreSQL unique-constraint error (23505).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return strings.Contains(err.Error(), "duplicate key")
}
