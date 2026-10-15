package cctest

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestStartGivingUpReturnsErrNoServer(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := start(ctx)
	if err == nil {
		t.Fatal("start with a cancelled context = nil error, want one")
	}
	if !errors.Is(err, ErrNoServer) {
		t.Errorf("start error = %v, want %v", err, ErrNoServer)
	}
}

func TestSweep(t *testing.T) {
	t.Parallel()
	DSN(t)
	s := shared
	now := time.Now()
	stale := scratchDatabase(t, s, now.Add(-2*staleAfter))
	fresh := scratchDatabase(t, s, now.Add(-time.Second))
	inUse := scratchDatabase(t, s, now.Add(-2*staleAfter-time.Second))
	conn, err := pgx.Connect(t.Context(), s.dsn(inUse))
	if err != nil {
		t.Fatalf("Connect(%s): %v", inUse, err)
	}
	t.Cleanup(func() {
		if err := conn.Close(context.WithoutCancel(t.Context())); err != nil {
			t.Errorf("close connection: %v", err)
		}
	})

	if err := s.sweep(t.Context(), now); err != nil {
		t.Fatalf("sweep(%s): %v", now, err)
	}
	for _, tt := range []struct {
		name string
		want bool
	}{
		{stale, false}, {fresh, true}, {inUse, true},
	} {
		var exists bool
		err := s.admin.QueryRow(t.Context(), "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", tt.name).
			Scan(&exists)
		if err != nil {
			t.Fatalf("Scan(exists for %s): %v", tt.name, err)
		}
		if exists != tt.want {
			t.Errorf("exists(%s) = %v, want %v", tt.name, exists, tt.want)
		}
	}
}

func scratchDatabase(t *testing.T, s *server, created time.Time) string {
	t.Helper()
	name := fmt.Sprintf("cc_test_%d_%08x", created.Unix(), uint32(created.UnixNano()))
	if _, err := s.admin.Exec(t.Context(), "CREATE DATABASE "+ident(name)); err != nil {
		t.Fatalf("Exec(CREATE DATABASE %s): %v", name, err)
	}
	t.Cleanup(func() {
		statement := "DROP DATABASE IF EXISTS " + ident(name) + " WITH (FORCE)"
		if _, err := s.admin.Exec(context.WithoutCancel(t.Context()), statement); err != nil {
			t.Errorf("drop %s: %v", name, err)
		}
	})
	return name
}
