package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/store/ccdb"
)

// CreateUser inserts one user row with an already-hashed password. The table's UNIQUE
// constraint on email fails this rather than silently replacing an existing account.
func (s *Store) CreateUser(ctx context.Context, email, passwordHash string, at time.Time) error {
	err := s.q.CreateUser(ctx, ccdb.CreateUserParams{
		Email:        email,
		PasswordHash: passwordHash,
		CreatedAt:    at.UTC(),
	})
	if err != nil {
		return fmt.Errorf("create user %s: %w", email, err)
	}
	return nil
}

// UserForLogin returns the row a login attempt verifies its password against.
func (s *Store) UserForLogin(ctx context.Context, email string) (ccdb.UserForLoginRow, error) {
	row, err := s.q.UserForLogin(ctx, email)
	if err != nil {
		return ccdb.UserForLoginRow{}, fmt.Errorf("select user %s: %w", email, err)
	}
	return row, nil
}

// DeleteExpiredSessions deletes every session whose expiry is at or before now, and returns how
// many rows that was.
func (s *Store) DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	deleted, err := s.q.DeleteExpiredSessions(ctx, now.UTC())
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	return deleted, nil
}

// SetPassword replaces email's password hash and deletes every session for that account.
func (s *Store) SetPassword(ctx context.Context, email, passwordHash string) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, tx.Rollback())
		}
	}()

	qtx := s.q.WithTx(tx)
	userID, err := qtx.UpdatePasswordByEmail(ctx, ccdb.UpdatePasswordByEmailParams{
		Email:        email,
		PasswordHash: passwordHash,
	})
	if err != nil {
		return fmt.Errorf("set password for %s: %w", email, err)
	}
	if err = qtx.DeleteSessionsForUser(ctx, userID); err != nil {
		return fmt.Errorf("delete sessions for %s: %w", email, err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// IssueSession replaces every session the user holds with one row for tokenSHA, so a token
// captured before a login is dead after it.
func (s *Store) IssueSession(ctx context.Context, userID int64, tokenSHA string, at, expiresAt time.Time) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, tx.Rollback())
		}
	}()

	qtx := s.q.WithTx(tx)
	if err = qtx.DeleteSessionsForUser(ctx, userID); err != nil {
		return fmt.Errorf("delete sessions for user %d: %w", userID, err)
	}
	err = qtx.IssueSession(ctx, ccdb.IssueSessionParams{
		UserID:    userID,
		TokenSHA:  tokenSHA,
		CreatedAt: at.UTC(),
		ExpiresAt: expiresAt.UTC(),
	})
	if err != nil {
		return fmt.Errorf("issue session for user %d: %w", userID, err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// SessionOwner returns the user id holding the unexpired session whose token hash is tokenSHA.
func (s *Store) SessionOwner(ctx context.Context, tokenSHA string, now time.Time) (int64, error) {
	id, err := s.q.SessionOwner(ctx, ccdb.SessionOwnerParams{TokenSHA: tokenSHA, ExpiresAt: now.UTC()})
	if err != nil {
		return 0, fmt.Errorf("select session owner: %w", err)
	}
	return id, nil
}

// DeleteSession removes the session whose token hash is tokenSHA.
func (s *Store) DeleteSession(ctx context.Context, tokenSHA string) error {
	if err := s.q.DeleteSession(ctx, tokenSHA); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}
