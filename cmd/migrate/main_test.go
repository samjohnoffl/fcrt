package main

import "testing"

func TestFileURL(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"file:///migrations", "file:///migrations"},
		{"/migrations", "file:///migrations"},
	}
	for _, tt := range tests {
		got, err := fileURL(tt.in)
		if err != nil {
			t.Fatalf("fileURL(%q): %v", tt.in, err)
		}
		if got != tt.want {
			t.Errorf("fileURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPGX5URL(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{
			"postgres://fcrt:fcrt@localhost:5432/db?sslmode=disable",
			"pgx5://fcrt:fcrt@localhost:5432/db?sslmode=disable",
		},
		{
			"postgresql://user@host/db",
			"pgx5://user@host/db",
		},
		{
			"pgx5://user@host/db",
			"pgx5://user@host/db",
		},
	}
	for _, tt := range tests {
		got, err := pgx5URL(tt.in)
		if err != nil {
			t.Fatalf("pgx5URL(%q): %v", tt.in, err)
		}
		if got != tt.want {
			t.Errorf("pgx5URL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
