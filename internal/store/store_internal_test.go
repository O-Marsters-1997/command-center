package store

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsUniqueViolation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"named constraint", &pgconn.PgError{Code: "23505", ConstraintName: "projects_name_key"}, true},
		{"wrapped", fmt.Errorf("insert: %w", &pgconn.PgError{Code: "23505", ConstraintName: "projects_name_key"}), true},
		{"other unique constraint", &pgconn.PgError{Code: "23505", ConstraintName: "repos_name_key"}, false},
		{"other violation", &pgconn.PgError{Code: "23503", ConstraintName: "projects_name_key"}, false},
		{"not a postgres error", errors.New("boom"), false},
		{"nil", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isUniqueViolation(tt.err, "projects_name_key"); got != tt.want {
				t.Errorf("isUniqueViolation(%v, projects_name_key) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
