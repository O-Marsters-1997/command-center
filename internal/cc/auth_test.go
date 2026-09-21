package cc_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/cctest"
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

func TestSetPasswordReplacesTheHashAndTheOldOneNoLongerVerifies(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)

	if err := store.CreateUser(ctx, "olly@example.com", "pbkdf2-sha256$600000$aa$bb", time.Now()); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := store.SetPassword(ctx, "olly@example.com", "pbkdf2-sha256$600000$cc$dd"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}

	row, err := store.UserForLogin(ctx, "olly@example.com")
	if err != nil {
		t.Fatalf("UserForLogin: %v", err)
	}
	if row.PasswordHash != "pbkdf2-sha256$600000$cc$dd" {
		t.Errorf("PasswordHash = %q, want the rotated hash", row.PasswordHash)
	}
}

func TestSetPasswordDeletesEverySessionForThatAccountButLeavesOthers(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	dsn := cctest.DSN(t)
	store := openStoreAt(t, dsn)

	if err := store.CreateUser(ctx, "olly@example.com", "pbkdf2-sha256$600000$aa$bb", time.Now()); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := store.CreateUser(ctx, "other@example.com", "pbkdf2-sha256$600000$aa$bb", time.Now()); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	target, err := store.UserForLogin(ctx, "olly@example.com")
	if err != nil {
		t.Fatalf("UserForLogin: %v", err)
	}
	other, err := store.UserForLogin(ctx, "other@example.com")
	if err != nil {
		t.Fatalf("UserForLogin: %v", err)
	}
	seedSession(t, dsn, target.ID, "target-token-sha")
	seedSession(t, dsn, other.ID, "other-token-sha")

	if err := store.SetPassword(ctx, "olly@example.com", "pbkdf2-sha256$600000$cc$dd"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}

	if got := sessionCountForUser(t, dsn, target.ID); got != 0 {
		t.Errorf("sessions for rotated account = %d, want 0", got)
	}
	if got := sessionCountForUser(t, dsn, other.ID); got != 1 {
		t.Errorf("sessions for other account = %d, want 1 (untouched)", got)
	}
}

func TestSetPasswordOnAnEmailWithNoAccountFailsRatherThanCreatingOne(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	dsn := cctest.DSN(t)
	store := openStoreAt(t, dsn)

	if err := store.SetPassword(ctx, "nobody@example.com", "pbkdf2-sha256$600000$cc$dd"); err == nil {
		t.Error("SetPassword for an unknown email = nil error, want a failure")
	}

	if got := userCount(t, dsn); got != 0 {
		t.Errorf("users after a failed SetPassword = %d, want 0", got)
	}
}

func TestSetPasswordRollsBackBothWritesOnFailure(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	dsn := cctest.DSN(t)
	store := openStoreAt(t, dsn)

	if err := store.CreateUser(ctx, "olly@example.com", "pbkdf2-sha256$600000$aa$bb", time.Now()); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	user, err := store.UserForLogin(ctx, "olly@example.com")
	if err != nil {
		t.Fatalf("UserForLogin: %v", err)
	}
	seedSession(t, dsn, user.ID, "target-token-sha")

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	lock, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin locking tx: %v", err)
	}
	if _, err := lock.ExecContext(ctx, `SELECT id FROM sessions WHERE user_id = $1 FOR UPDATE`, user.ID); err != nil {
		t.Fatalf("lock session row: %v", err)
	}
	t.Cleanup(func() { _ = lock.Rollback() })

	blocked, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := store.SetPassword(blocked, "olly@example.com", "pbkdf2-sha256$600000$cc$dd"); err == nil {
		t.Fatal("SetPassword while the session row is locked = nil error, want the blocked delete to fail")
	}

	if err := lock.Rollback(); err != nil {
		t.Fatalf("release lock: %v", err)
	}

	row, err := store.UserForLogin(ctx, "olly@example.com")
	if err != nil {
		t.Fatalf("UserForLogin: %v", err)
	}
	if row.PasswordHash != "pbkdf2-sha256$600000$aa$bb" {
		t.Errorf("PasswordHash = %q, the update was not rolled back with the failed session delete", row.PasswordHash)
	}
	if got := sessionCountForUser(t, dsn, user.ID); got != 1 {
		t.Errorf("sessions for the account = %d, want 1 (the failed delete left it in place)", got)
	}
}

func seedSession(t *testing.T, dsn string, userID int64, tokenSHA string) {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	now := time.Now().UTC()
	_, err = db.Exec(
		`INSERT INTO sessions (user_id, token_sha, created_at, expires_at) VALUES ($1, $2, $3, $4)`,
		userID, tokenSHA, now, now.Add(24*time.Hour),
	)
	if err != nil {
		t.Fatalf("seed session for user %d: %v", userID, err)
	}
}

func sessionCountForUser(t *testing.T, dsn string, userID int64) int {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	var count int
	if err := db.QueryRow(`SELECT count(*) FROM sessions WHERE user_id = $1`, userID).Scan(&count); err != nil {
		t.Fatalf("count sessions for user %d: %v", userID, err)
	}
	return count
}

func userCount(t *testing.T, dsn string) int {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	var count int
	if err := db.QueryRow(`SELECT count(*) FROM users`).Scan(&count); err != nil {
		t.Fatalf("count users: %v", err)
	}
	return count
}
