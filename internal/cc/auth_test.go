package cc_test

import (
	"testing"
	"time"
)

func TestCreateUserThenUserForLoginRoundTrips(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)

	if err := store.CreateUser(ctx, "olly@example.com", "pbkdf2-sha256$600000$aa$bb", time.Now()); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	row, err := store.UserForLogin(ctx, "olly@example.com")
	if err != nil {
		t.Fatalf("UserForLogin: %v", err)
	}
	if row.Email != "olly@example.com" {
		t.Errorf("Email = %q, want olly@example.com", row.Email)
	}
	if row.PasswordHash != "pbkdf2-sha256$600000$aa$bb" {
		t.Errorf("PasswordHash = %q, want the stored hash", row.PasswordHash)
	}
}

func TestCreateUserTwiceForTheSameEmailFails(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)

	if err := store.CreateUser(ctx, "olly@example.com", "pbkdf2-sha256$600000$aa$bb", time.Now()); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := store.CreateUser(ctx, "olly@example.com", "pbkdf2-sha256$600000$cc$dd", time.Now()); err == nil {
		t.Error("CreateUser for a duplicate email = nil error, want a failure rather than a silent replace")
	}

	row, err := store.UserForLogin(ctx, "olly@example.com")
	if err != nil {
		t.Fatalf("UserForLogin: %v", err)
	}
	if row.PasswordHash != "pbkdf2-sha256$600000$aa$bb" {
		t.Errorf("PasswordHash = %q, the first account was replaced", row.PasswordHash)
	}
}

func TestUserForLoginOnUnknownEmailFails(t *testing.T) {
	t.Parallel()

	store := openStore(t)

	if _, err := store.UserForLogin(t.Context(), "nobody@example.com"); err == nil {
		t.Error("UserForLogin for an unknown email = nil error, want a failure")
	}
}
