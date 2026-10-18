// Package auth — password_test.go
//
// Hermetic tests for password hashing. No database required.
package auth

import (
	"strings"
	"testing"
)

func TestHashPassword_RoundTrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if strings.Contains(hash, "correct horse") {
		t.Fatal("hash must not contain the plaintext password")
	}
	if !VerifyPassword(hash, "correct horse battery staple") {
		t.Error("the correct password must verify")
	}
	if VerifyPassword(hash, "wrong password") {
		t.Error("a wrong password must not verify")
	}
	if VerifyPassword("not-a-bcrypt-hash", "anything") {
		t.Error("a malformed hash must never verify")
	}
}

func TestHashPassword_RejectsTooShort(t *testing.T) {
	if _, err := HashPassword(strings.Repeat("a", MinPasswordLen-1)); err == nil {
		t.Errorf("passwords shorter than %d characters must be rejected", MinPasswordLen)
	}
	if _, err := HashPassword(strings.Repeat("a", MinPasswordLen)); err != nil {
		t.Errorf("a %d-character password must be accepted: %v", MinPasswordLen, err)
	}
}

func TestHashPassword_UsesAUniqueSalt(t *testing.T) {
	a, err := HashPassword("same-password")
	if err != nil {
		t.Fatal(err)
	}
	b, err := HashPassword("same-password")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("the same password must hash differently each time (random salt)")
	}
}

func TestHashPassword_RejectsEmptyEvenWhenUnchecked(t *testing.T) {
	// Bootstrap seeding bypasses the length policy but must never accept an
	// empty password.
	if _, err := hashPassword(""); err == nil {
		t.Error("an empty password must be rejected")
	}
	hash, err := hashPassword("pw") // shorter than MinPasswordLen, allowed for seeds
	if err != nil {
		t.Fatalf("hashPassword short: %v", err)
	}
	if !VerifyPassword(hash, "pw") {
		t.Error("a seeded short password must verify")
	}
}

func TestValidRole(t *testing.T) {
	for _, role := range []string{RoleAnalyst, RoleAdmin} {
		if !ValidRole(role) {
			t.Errorf("%q must be a valid role", role)
		}
	}
	for _, role := range []string{"", "viewer", "superuser", "ADMIN"} {
		if ValidRole(role) {
			t.Errorf("%q must not be a valid role", role)
		}
	}
}
