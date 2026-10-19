package campaign

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yourorg/financial-crime-red-team/internal/aml"
)

func openTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}
	url := os.Getenv("DATABASE_TEST_URL")
	if url == "" {
		url = "host=/var/run/postgresql dbname=fincrime_test sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Skipf("cannot connect to test DB: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("test DB not reachable: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestCampaignCreateGetListUpdate(t *testing.T) {
	pool := openTestPool(t)
	repo := NewRepository(pool)
	ctx := context.Background()

	created, err := repo.Create(ctx, Campaign{
		Name:          "Q1 wire campaign",
		Description:   "test",
		Goal:          "exercise counterparty expansion",
		Budget:        5,
		ControlConfig: aml.DefaultControlConfig(),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM campaign WHERE id=$1`, created.ID) })

	if created.ID == uuid.Nil {
		t.Fatal("Create returned a nil ID")
	}
	if created.Status != StatusActive {
		t.Errorf("status=%q, want active", created.Status)
	}
	if created.ControlConfig == nil {
		t.Error("control config not round-tripped")
	}

	got, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != created.Name || got.Budget != 5 {
		t.Errorf("Get returned %+v, want name=%q budget=5", got, created.Name)
	}

	list, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, c := range list {
		if c.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Error("created campaign missing from List")
	}

	newBudget := 8
	newStatus := StatusPaused
	updated, err := repo.Update(ctx, created.ID, nil, &newBudget, &newStatus)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Budget != 8 || updated.Status != StatusPaused {
		t.Errorf("updated budget=%d status=%q, want 8/paused", updated.Budget, updated.Status)
	}
}

func TestCampaignBudgetIsEnforced(t *testing.T) {
	pool := openTestPool(t)
	repo := NewRepository(pool)
	ctx := context.Background()

	c, err := repo.Create(ctx, Campaign{Name: "tight budget", Budget: 1})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM campaign WHERE id=$1`, c.ID) })

	if err := repo.CanRun(ctx, c.ID); err != nil {
		t.Fatalf("CanRun before use: %v", err)
	}
	if err := repo.IncrementUsed(ctx, c.ID); err != nil {
		t.Fatalf("IncrementUsed: %v", err)
	}
	if err := repo.CanRun(ctx, c.ID); !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("CanRun after budget used = %v, want ErrBudgetExhausted", err)
	}
	if err := repo.IncrementUsed(ctx, c.ID); !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("IncrementUsed past budget = %v, want ErrBudgetExhausted", err)
	}
}

func TestCampaignGetMissing(t *testing.T) {
	pool := openTestPool(t)
	repo := NewRepository(pool)
	if _, err := repo.Get(context.Background(), uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get missing = %v, want ErrNotFound", err)
	}
}
