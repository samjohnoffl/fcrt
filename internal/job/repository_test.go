// Package job — repository_test.go
//
// Integration tests for the job queue. These require a real PostgreSQL database
// with the schema migrated. They are skipped in -short mode and when no DB is
// reachable, so `go test -short ./...` stays hermetic.
package job

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
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

func TestEnqueueCreatesPendingJob(t *testing.T) {
	pool := openTestPool(t)
	repo := NewRepository(pool)
	ctx := context.Background()

	cohortID := uuid.New()
	id, err := repo.Enqueue(ctx, TypeGenerateCohort, GenerateCohortPayload{
		CohortID:   cohortID,
		Seed:       99,
		WindowDays: 90,
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	t.Cleanup(func() { pool.Exec(ctx, `DELETE FROM job WHERE id = $1`, id) })

	var (
		jobType string
		status  string
		payload []byte
	)
	if err := pool.QueryRow(ctx,
		`SELECT job_type, status, payload FROM job WHERE id = $1`, id).
		Scan(&jobType, &status, &payload); err != nil {
		t.Fatalf("select job: %v", err)
	}

	if jobType != string(TypeGenerateCohort) {
		t.Errorf("job_type = %q, want %q", jobType, TypeGenerateCohort)
	}
	if status != "pending" {
		t.Errorf("status = %q, want pending", status)
	}
	if !strings.Contains(string(payload), cohortID.String()) {
		t.Errorf("payload %s does not contain cohort id", payload)
	}
	if !strings.Contains(string(payload), "window_days") {
		t.Errorf("payload %s missing window_days", payload)
	}
}
