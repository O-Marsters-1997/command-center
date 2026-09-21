package cc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc/ccdb"
)

const sessionLifetime = 30 * 24 * time.Hour

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

// IssueSession stores tokenHash, the SHA-256 hash of a session token, against userID with a
// fixed sessionLifetime expiry from issuedAt.
func (s *Store) IssueSession(ctx context.Context, userID int64, tokenHash string, issuedAt time.Time) error {
	err := s.q.IssueSession(ctx, ccdb.IssueSessionParams{
		UserID:    userID,
		TokenSHA:  tokenHash,
		CreatedAt: issuedAt.UTC(),
		ExpiresAt: issuedAt.UTC().Add(sessionLifetime),
	})
	if err != nil {
		return fmt.Errorf("issue session: %w", err)
	}
	return nil
}

// SessionOwner returns the user id owning the session whose token hashes to tokenHash, unless it
// has expired by at.
func (s *Store) SessionOwner(ctx context.Context, tokenHash string, at time.Time) (int64, error) {
	userID, err := s.q.SessionOwner(ctx, ccdb.SessionOwnerParams{TokenSHA: tokenHash, ExpiresAt: at.UTC()})
	if err != nil {
		return 0, fmt.Errorf("select session owner: %w", err)
	}
	return userID, nil
}

// DeleteSession removes every session row belonging to userID.
func (s *Store) DeleteSession(ctx context.Context, userID int64) error {
	if err := s.q.DeleteSession(ctx, userID); err != nil {
		return fmt.Errorf("delete sessions for user %d: %w", userID, err)
	}
	return nil
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
	if err = qtx.DeleteSession(ctx, userID); err != nil {
		return fmt.Errorf("delete sessions for %s: %w", email, err)
	}
	return tx.Commit()
}
