// Package cctest hands a test its own empty Postgres database.
package cctest

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

const (
	serverEnv    = "CC_TEST_DATABASE_URL"
	startTimeout = 2 * time.Minute
)

var (
	serverOnce sync.Once
	serverDSN  string
	serverErr  error
	nextDB     atomic.Int64
)

func server() (string, error) {
	serverOnce.Do(func() {
		if dsn := os.Getenv(serverEnv); dsn != "" {
			serverDSN = dsn
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
		defer cancel()
		serverDSN, serverErr = startContainer(ctx)
	})
	return serverDSN, serverErr
}

func startContainer(ctx context.Context) (string, error) {
	container, err := postgres.Run(ctx, "postgres:17",
		postgres.WithDatabase("cc"),
		postgres.WithUsername("cc"),
		postgres.WithPassword("cc"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		return "", fmt.Errorf("start postgres container (set %s to use a running server): %w", serverEnv, err)
	}
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return "", fmt.Errorf("read postgres container address (set %s to use a running server): %w", serverEnv, err)
	}
	return dsn, nil
}

// DSN returns a connection string for an empty database, dropped when the test ends.
func DSN(t *testing.T) string {
	t.Helper()
	dsn, drop, err := Create()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := drop(); err != nil {
			t.Errorf("drop test database: %v", err)
		}
	})
	return dsn
}

// Create returns a connection string for an empty database and the function that drops it.
// e2e's testscript setup has no *testing.T to hang a cleanup off, so it calls this instead of DSN.
func Create() (dsn string, drop func() error, err error) {
	admin, err := server()
	if err != nil {
		return "", nil, fmt.Errorf("find test server: %w", err)
	}
	// The pid keeps two test binaries sharing one CC_TEST_DATABASE_URL server from colliding.
	name := fmt.Sprintf("cc_test_%d_%d", os.Getpid(), nextDB.Add(1))
	if err := exec(admin, "CREATE DATABASE "+name); err != nil {
		return "", nil, fmt.Errorf("create test database %s: %w", name, err)
	}

	dsn, err = withDatabase(admin, name)
	if err != nil {
		return "", nil, fmt.Errorf("address test database %s: %w", name, err)
	}
	return dsn, func() error {
		// FORCE rather than a bare DROP: a daemon an e2e script left running still holds
		// connections when the script's cleanup runs.
		return exec(admin, "DROP DATABASE "+name+" WITH (FORCE)")
	}, nil
}

func exec(dsn, statement string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", serverEnv, err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(statement); err != nil {
		return fmt.Errorf("%s: %w", statement, err)
	}
	return nil
}

func withDatabase(dsn, name string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", serverEnv, err)
	}
	u.Path = "/" + name
	return u.String(), nil
}
