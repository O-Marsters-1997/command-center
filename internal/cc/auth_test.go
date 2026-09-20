package cc_test

import (
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
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

func createTestUser(t *testing.T, store *cc.Store, email string) int64 {
	t.Helper()

	if err := store.CreateUser(t.Context(), email, "pbkdf2-sha256$600000$aa$bb", time.Now()); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	row, err := store.UserForLogin(t.Context(), email)
	if err != nil {
		t.Fatalf("UserForLogin: %v", err)
	}
	return row.ID
}

func TestIssueSessionThenSessionOwnerRoundTrips(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	userID := createTestUser(t, store, "olly@example.com")
	issuedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	if err := store.IssueSession(t.Context(), userID, "deadbeef", issuedAt); err != nil {
		t.Fatalf("IssueSession: %v", err)
	}

	got, err := store.SessionOwner(t.Context(), "deadbeef", issuedAt)
	if err != nil {
		t.Fatalf("SessionOwner: %v", err)
	}
	if got != userID {
		t.Errorf("SessionOwner() = %d, want %d", got, userID)
	}
}

func TestSessionOwnerOnExpiredSessionFails(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	userID := createTestUser(t, store, "olly@example.com")
	issuedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	if err := store.IssueSession(t.Context(), userID, "deadbeef", issuedAt); err != nil {
		t.Fatalf("IssueSession: %v", err)
	}

	afterExpiry := issuedAt.Add(31 * 24 * time.Hour)
	if _, err := store.SessionOwner(t.Context(), "deadbeef", afterExpiry); err == nil {
		t.Error("SessionOwner() past its expiry = nil error, want a failure")
	}
}

func TestSessionOwnerOnUnknownTokenFails(t *testing.T) {
	t.Parallel()

	store := openStore(t)

	if _, err := store.SessionOwner(t.Context(), "nope", time.Now()); err == nil {
		t.Error("SessionOwner() for an unknown token = nil error, want a failure")
	}
}

func TestDeleteSessionRemovesEveryRowForTheUser(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	userID := createTestUser(t, store, "olly@example.com")
	issuedAt := time.Now()

	if err := store.IssueSession(t.Context(), userID, "first", issuedAt); err != nil {
		t.Fatalf("IssueSession: %v", err)
	}
	if err := store.IssueSession(t.Context(), userID, "second", issuedAt); err != nil {
		t.Fatalf("IssueSession: %v", err)
	}
	if err := store.DeleteSession(t.Context(), userID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	if _, err := store.SessionOwner(t.Context(), "first", issuedAt); err == nil {
		t.Error("SessionOwner() for a deleted session = nil error, want a failure")
	}
	if _, err := store.SessionOwner(t.Context(), "second", issuedAt); err == nil {
		t.Error("SessionOwner() for a deleted session = nil error, want a failure")
	}
}
