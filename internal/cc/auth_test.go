package cc_test

import (
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
