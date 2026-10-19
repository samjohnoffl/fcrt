package experiment

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestRepository_ReviewDecisionLifecycle(t *testing.T) {
	pool := openTestPool(t)
	repo := NewRepository(pool)
	ctx := context.Background()

	cohortID := seedCohort(t, pool)
	res := buildExecuteResult(t, cohortID, 99)
	if err := repo.SavePairResult(ctx, res); err != nil {
		t.Fatalf("SavePairResult: %v", err)
	}
	pairID := res.Pair.ID

	queue, err := repo.ListReviewQueue(ctx, 100)
	if err != nil {
		t.Fatalf("ListReviewQueue: %v", err)
	}
	found := false
	for _, item := range queue {
		if item.Pair.ID == pairID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("completed pair missing from the review queue")
	}

	saved, err := repo.SaveReview(ctx, ReviewDecision{
		PairID:    pairID,
		Reviewer:  "analyst",
		Decision:  ReviewRejected,
		Rationale: "Outcome unchanged; no new evidence.",
	})
	if err != nil {
		t.Fatalf("SaveReview: %v", err)
	}
	if saved.ID == uuid.Nil || saved.CreatedAt.IsZero() {
		t.Error("saved review is missing generated fields")
	}

	got, err := repo.GetReview(ctx, pairID)
	if err != nil {
		t.Fatalf("GetReview: %v", err)
	}
	if got.Decision != ReviewRejected || got.Reviewer != "analyst" {
		t.Errorf("got %+v, want rejected by analyst", got)
	}

	if _, err := repo.SaveReview(ctx, ReviewDecision{
		PairID:    pairID,
		Reviewer:  "admin",
		Decision:  ReviewApproved,
		Rationale: "Second opinion",
	}); !errors.Is(err, ErrAlreadyReviewed) {
		t.Fatalf("second SaveReview = %v, want ErrAlreadyReviewed", err)
	}

	queue, err = repo.ListReviewQueue(ctx, 100)
	if err != nil {
		t.Fatalf("ListReviewQueue after review: %v", err)
	}
	for _, item := range queue {
		if item.Pair.ID == pairID {
			t.Fatal("reviewed pair is still in the review queue")
		}
	}
}

func TestRepository_OutcomesRoundTrip(t *testing.T) {
	pool := openTestPool(t)
	repo := NewRepository(pool)
	ctx := context.Background()

	cohortID := seedCohort(t, pool)
	res := buildExecuteResult(t, cohortID, 101)
	if err := repo.SavePairResult(ctx, res); err != nil {
		t.Fatalf("SavePairResult: %v", err)
	}

	runs, err := repo.ListRunsForPair(ctx, res.Pair.ID)
	if err != nil {
		t.Fatalf("ListRunsForPair: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("runs=%d, want 2", len(runs))
	}
	for _, run := range runs {
		if len(run.Outcomes) != 8 {
			t.Errorf("%s run has %d outcomes, want 8 (one per control family)", run.RunType, len(run.Outcomes))
		}
		for _, oc := range run.Outcomes {
			if oc.Status == "" || oc.ReasonCode == "" {
				t.Errorf("%s run outcome %s is missing status/reason", run.RunType, oc.RuleID)
			}
			if oc.RuleVersion != res.Pair.ControlHash {
				t.Errorf("%s run outcome %s rule_version=%q, want %q",
					run.RunType, oc.RuleID, oc.RuleVersion, res.Pair.ControlHash)
			}
		}
	}
}

func TestRepository_ReviewQueueSkipsIncompletePairs(t *testing.T) {
	pool := openTestPool(t)
	repo := NewRepository(pool)
	ctx := context.Background()

	cohortID := seedCohort(t, pool)
	res := buildExecuteResult(t, cohortID, 100)
	res.Pair.Status = StatusRunning
	if err := repo.SavePairResult(ctx, res); err != nil {
		t.Fatalf("SavePairResult: %v", err)
	}

	queue, err := repo.ListReviewQueue(ctx, 100)
	if err != nil {
		t.Fatalf("ListReviewQueue: %v", err)
	}
	for _, item := range queue {
		if item.Pair.ID == res.Pair.ID {
			t.Fatal("incomplete pair appeared in the review queue")
		}
	}
}
