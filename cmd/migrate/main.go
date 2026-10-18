// Command migrate applies the SQL migrations in migrations/ to the database
// referenced by DATABASE_URL.
//
// It wraps golang-migrate (per AGENTS.md: "Migrations: golang-migrate (SQL
// files in migrations/)") so the exact same tool runs locally and in the Cloud
// Run migration job (scripts/gcp-setup.sh invokes /migrate inside the image).
//
// It is compatible with the schema_migrations table written by the official
// migrate/migrate image used earlier in local development, so it can take over
// an existing database without re-running migrations.
//
// Usage:
//
//	DATABASE_URL=postgres://user:pass@host:5432/db /migrate          # apply up
//	MIGRATIONS_DIR=./migrations /migrate up                        # explicit
//	DATABASE_URL=... /migrate version                              # show version
//
// Exit status is 0 when migrations are applied (or already current) and
// non-zero on any failure.
package main

import (
	"errors"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // registers the "pgx5" scheme
	_ "github.com/golang-migrate/migrate/v4/source/file"     // registers the "file" source
)

func main() {
	log.SetFlags(0)

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("migrate: DATABASE_URL is required")
	}

	dir := os.Getenv("MIGRATIONS_DIR")
	if dir == "" {
		dir = "/migrations"
	}
	sourceURL, err := fileURL(dir)
	if err != nil {
		log.Fatalf("migrate: resolve migrations dir %q: %v", dir, err)
	}

	// The pgx v5 driver registers the "pgx5" scheme; rewrite a standard
	// postgres:// URL so callers keep using DATABASE_URL unchanged.
	migrateURL, err := pgx5URL(dbURL)
	if err != nil {
		log.Fatalf("migrate: parse DATABASE_URL: %v", err)
	}

	m, err := migrate.New(sourceURL, migrateURL)
	if err != nil {
		log.Fatalf("migrate: init: %v", err)
	}
	defer func() {
		if srcErr, dbErr := m.Close(); srcErr != nil || dbErr != nil {
			log.Printf("migrate: close: src=%v db=%v", srcErr, dbErr)
		}
	}()

	cmd := "up"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	switch cmd {
	case "up":
		if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			log.Fatalf("migrate up: %v", err)
		}
		report(m, "up")
	case "down":
		// One step only — `reset` is the explicit "drop everything" command.
		if err := m.Steps(-1); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			log.Fatalf("migrate down: %v", err)
		}
		report(m, "down")
	case "reset":
		if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			log.Fatalf("migrate reset: %v", err)
		}
		report(m, "reset")
	case "version":
		v, dirty, err := m.Version()
		if err != nil {
			if errors.Is(err, migrate.ErrNilVersion) {
				log.Println("migrate: no version applied")
				return
			}
			log.Fatalf("migrate version: %v", err)
		}
		log.Printf("migrate: version=%d dirty=%v", v, dirty)
	default:
		log.Fatalf("migrate: unknown command %q (want up|down|reset|version)", cmd)
	}
}

func report(m *migrate.Migrate, action string) {
	v, dirty, err := m.Version()
	if err != nil {
		log.Printf("migrate %s: done", action)
		return
	}
	log.Printf("migrate %s: done (version=%d dirty=%v)", action, v, dirty)
}

// fileURL converts a filesystem path to a file:// source URL.
func fileURL(dir string) (string, error) {
	if strings.HasPrefix(dir, "file://") {
		return dir, nil
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	return "file://" + abs, nil
}

// pgx5URL rewrites postgres:// and postgresql:// schemes to pgx5:// so the
// golang-migrate pgx v5 database driver handles the connection.
func pgx5URL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "pgx5":
		return raw, nil
	case "postgres", "postgresql":
		u.Scheme = "pgx5"
		return u.String(), nil
	default:
		return raw, nil
	}
}
