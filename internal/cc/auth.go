package cc

import (
	"context"
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc/ccdb"
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
