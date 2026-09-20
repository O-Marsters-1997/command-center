package cc_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

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

func TestDeleteExpiredSessionsRemovesOnlyThePastRow(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	dsn := cctest.DSN(t)
	store := openStoreAt(t, dsn)

	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	userID := seedUser(t, dsn, "olly@example.com")
	seedSession(t, dsn, userID, "expired-token", now.Add(-time.Hour))
	seedSession(t, dsn, userID, "live-token", now.Add(time.Hour))

	deleted, err := store.DeleteExpiredSessions(ctx, now)
	if err != nil {
		t.Fatalf("DeleteExpiredSessions: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}

	remaining := sessionTokens(t, dsn)
	if len(remaining) != 1 || remaining[0] != "live-token" {
		t.Errorf("remaining sessions = %v, want only live-token", remaining)
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
	expiresAt := time.Now().Add(24 * time.Hour)
	seedSession(t, dsn, target.ID, "target-token-sha", expiresAt)
	seedSession(t, dsn, other.ID, "other-token-sha", expiresAt)

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
	seedSession(t, dsn, user.ID, "target-token-sha", time.Now().Add(24*time.Hour))

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

func seedUser(t *testing.T, dsn, email string) int64 {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	var id int64
	query := `INSERT INTO users (email, password_hash, created_at) VALUES ($1, 'hash', now()) RETURNING id`
	if err := db.QueryRow(query, email).Scan(&id); err != nil {
		t.Fatalf("seed user %s: %v", email, err)
	}
	return id
}

func seedSession(t *testing.T, dsn string, userID int64, tokenSHA string, expiresAt time.Time) {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	query := `INSERT INTO sessions (user_id, token_sha, created_at, expires_at) VALUES ($1, $2, now(), $3)`
	if _, err := db.Exec(query, userID, tokenSHA, expiresAt); err != nil {
		t.Fatalf("seed session %s: %v", tokenSHA, err)
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

func sessionTokens(t *testing.T, dsn string) []string {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	rows, err := db.Query(`SELECT token_sha FROM sessions ORDER BY token_sha`)
	if err != nil {
		t.Fatalf("query sessions: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var tokens []string
	for rows.Next() {
		var token string
		if err := rows.Scan(&token); err != nil {
			t.Fatalf("scan session token: %v", err)
		}
		tokens = append(tokens, token)
	}
	return tokens
}
