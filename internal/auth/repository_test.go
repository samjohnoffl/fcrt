// Package auth — repository_test.go
//
// Integration tests for the PostgreSQL user directory. They require a real
// database with the schema migrated and are skipped in -short mode or when no
// database is reachable, so `go test -short ./...` stays hermetic.
package auth

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func testDBURL() string {
	if url := os.Getenv("DATABASE_TEST_URL"); url != "" {
		return url
	}
	return "postgres://localhost/fincrime_test?sslmode=disable"
}

func openTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, testDBURL())
	if err != nil {
		t.Skipf("skipping: cannot create pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("skipping: database not reachable: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// seed creates a user with a unique name and removes it afterwards.
func seed(t *testing.T, repo *Repository, pool *pgxpool.Pool, username, password, role string) {
	t.Helper()
	ctx := context.Background()
	t.Cleanup(func() { pool.Exec(ctx, `DELETE FROM app_user WHERE username = $1`, username) })
	if err := repo.EnsureUser(ctx, username, password, role); err != nil {
		t.Fatalf("EnsureUser(%s): %v", username, err)
	}
}

func TestRepository_Authenticate(t *testing.T) {
	pool := openTestPool(t)
	repo := NewRepository(pool)
	ctx := context.Background()
	seed(t, repo, pool, "it_alice", "s3cret-pass", RoleAnalyst)

	u, ok, err := repo.Authenticate(ctx, "it_alice", "s3cret-pass")
	if err != nil || !ok {
		t.Fatalf("Authenticate: ok=%v err=%v", ok, err)
	}
	if u.Username != "it_alice" || u.Role != RoleAnalyst {
		t.Errorf("got %+v, want it_alice/analyst", u)
	}

	if _, ok, err := repo.Authenticate(ctx, "it_alice", "wrong"); err != nil || ok {
		t.Errorf("wrong password: ok=%v err=%v, want false/nil", ok, err)
	}
	if _, ok, err := repo.Authenticate(ctx, "it_nobody", "x"); err != nil || ok {
		t.Errorf("unknown user: ok=%v err=%v, want false/nil", ok, err)
	}
}

func TestRepository_EnsureUserIsIdempotent(t *testing.T) {
	pool := openTestPool(t)
	repo := NewRepository(pool)
	ctx := context.Background()
	seed(t, repo, pool, "it_bob", "first-password", RoleAnalyst)

	// Seeding again must not clobber the existing account.
	if err := repo.EnsureUser(ctx, "it_bob", "second-password", RoleAdmin); err != nil {
		t.Fatalf("EnsureUser again: %v", err)
	}
	if _, ok, _ := repo.Authenticate(ctx, "it_bob", "first-password"); !ok {
		t.Error("the original password must still work after a re-seed")
	}
	if u, _ := repo.Get(ctx, "it_bob"); u.Role != RoleAnalyst {
		t.Errorf("role = %q, want analyst (re-seed must not change it)", u.Role)
	}
}

func TestRepository_CreateDuplicateAndValidation(t *testing.T) {
	pool := openTestPool(t)
	repo := NewRepository(pool)
	ctx := context.Background()
	seed(t, repo, pool, "it_carol", "pw-123456", RoleAnalyst)

	if _, err := repo.Create(ctx, "it_carol", "pw-123456", RoleAnalyst); !errors.Is(err, ErrUsernameTaken) {
		t.Errorf("duplicate create: err=%v, want ErrUsernameTaken", err)
	}
	if _, err := repo.Create(ctx, "it_invalid_role", "pw-123456", "viewer"); !errors.Is(err, ErrInvalidRole) {
		t.Errorf("invalid role: err=%v, want ErrInvalidRole", err)
	}
	if _, err := repo.Create(ctx, "it_short_pw", "123", RoleAnalyst); err == nil {
		t.Error("a too-short password must be rejected")
	}

	// A created user appears in List.
	created, err := repo.Create(ctx, "it_dave", "pw-123456", RoleAdmin)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { pool.Exec(ctx, `DELETE FROM app_user WHERE username = $1`, "it_dave") })
	if created.Role != RoleAdmin {
		t.Errorf("role = %q, want admin", created.Role)
	}
	users, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, u := range users {
		if u.Username == "it_dave" {
			found = true
		}
	}
	if !found {
		t.Error("it_dave must appear in List")
	}
}

func TestRepository_SeedAllowsShortBootstrapPassword(t *testing.T) {
	pool := openTestPool(t)
	repo := NewRepository(pool)
	ctx := context.Background()

	// The user-management policy rejects short passwords...
	if _, err := repo.Create(ctx, "it_short_boot", "pw", RoleAnalyst); err == nil {
		t.Error("Create must enforce the minimum password length")
	}
	// ...but seeding the built-in accounts accepts them, so the documented
	// test credentials (admin/admin) work out of the box.
	seed(t, repo, pool, "it_short_boot", "pw", RoleAnalyst)
	if _, ok, err := repo.Authenticate(ctx, "it_short_boot", "pw"); err != nil || !ok {
		t.Errorf("seeded account must authenticate: ok=%v err=%v", ok, err)
	}
}

func TestRepository_UpdateAndDelete(t *testing.T) {
	pool := openTestPool(t)
	repo := NewRepository(pool)
	ctx := context.Background()
	seed(t, repo, pool, "it_erin", "pw-123456", RoleAnalyst)

	adminRole := RoleAdmin
	u, err := repo.Update(ctx, "it_erin", &adminRole, nil, nil)
	if err != nil {
		t.Fatalf("Update role: %v", err)
	}
	if u.Role != RoleAdmin {
		t.Errorf("role = %q, want admin", u.Role)
	}

	// Reset the password.
	newPw := "brand-new-password"
	if _, err := repo.Update(ctx, "it_erin", nil, &newPw, nil); err != nil {
		t.Fatalf("Update password: %v", err)
	}
	if _, ok, _ := repo.Authenticate(ctx, "it_erin", newPw); !ok {
		t.Error("the new password must work")
	}

	// Disable the account: authentication must fail, but the row remains.
	yes := true
	if _, err := repo.Update(ctx, "it_erin", nil, nil, &yes); err != nil {
		t.Fatalf("Update disabled: %v", err)
	}
	if _, ok, _ := repo.Authenticate(ctx, "it_erin", newPw); ok {
		t.Error("a disabled account must not authenticate")
	}

	// Update of an unknown user.
	if _, err := repo.Update(ctx, "it_ghost", &adminRole, nil, nil); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("unknown update: err=%v, want ErrUserNotFound", err)
	}

	if err := repo.Delete(ctx, "it_erin"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := repo.Delete(ctx, "it_erin"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("delete twice: err=%v, want ErrUserNotFound", err)
	}
}
