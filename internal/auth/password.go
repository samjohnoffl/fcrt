// Package auth owns application-user credentials: password hashing and the
// PostgreSQL-backed user directory used by AUTH_MODE=local.
//
// Passwords are stored as bcrypt hashes and are never logged or returned.
package auth

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// MinPasswordLen is the shortest password accepted by user management
// (POST /users, password reset). It does not apply to bootstrap accounts seeded
// from AUTH_USERS, which are operator-provided configuration.
const MinPasswordLen = 6

// HashPassword returns a bcrypt hash of password, enforcing the length policy.
func HashPassword(password string) (string, error) {
	if len(password) < MinPasswordLen {
		return "", fmt.Errorf("auth: password must be at least %d characters", MinPasswordLen)
	}
	return hashPassword(password)
}

// hashPassword hashes without applying the length policy. Used to seed the
// built-in accounts from AUTH_USERS, whose passwords (e.g. the documented
// admin/admin test credentials) may be shorter than the creation policy.
func hashPassword(password string) (string, error) {
	if password == "" {
		return "", fmt.Errorf("auth: password must not be empty")
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("auth: hash password: %w", err)
	}
	return string(h), nil
}

// VerifyPassword reports whether password matches a stored bcrypt hash.
func VerifyPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// dummyHash is a valid bcrypt hash compared against when a username does not
// exist, so that a missing user takes about the same time as a wrong password.
var dummyHash = func() string {
	h, err := bcrypt.GenerateFromPassword([]byte("timing-equalizer"), bcrypt.DefaultCost)
	if err != nil {
		panic("auth: cannot initialise timing hash: " + err.Error())
	}
	return string(h)
}()
