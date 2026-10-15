// cmd/worker/main.go — Background job worker entry point.
// Polls the `job` table and dispatches work to internal packages.
// DECISION-004: Uses PostgreSQL-backed polling (option A).
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel(),
	}))
	slog.SetDefault(logger)

	// TODO: load config from env
	// TODO: open database connection (pgx pool)
	// TODO: build job dispatcher:
	//         job_type "generate_cohort"    → internal/bank.Generator.Run
	//         job_type "run_experiment"     → internal/experiment.Runner.Run
	//         job_type "recompute_coverage" → internal/coverage.Recomputer.Run

	pollInterval := pollIntervalFromEnv()
	slog.Info("worker starting", "poll_interval", pollInterval)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// TODO: SELECT ... FOR UPDATE SKIP LOCKED on job table
				// TODO: dispatch to handler, update status, handle errors + backoff
				slog.Debug("poll tick")
			}
		}
	}()

	<-stop
	slog.Info("worker shutting down")
	cancel()
}

func logLevel() slog.Level {
	switch os.Getenv("LOG_LEVEL") {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func pollIntervalFromEnv() time.Duration {
	v := os.Getenv("WORKER_POLL_INTERVAL_SECONDS")
	if v == "" {
		return 5 * time.Second
	}
	d, err := time.ParseDuration(v + "s")
	if err != nil {
		return 5 * time.Second
	}
	return d
}
