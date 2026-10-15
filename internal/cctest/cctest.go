package cctest

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moby/moby/api/types/container"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/O-Marsters-1997/command-center/internal/store"
)

const (
	serverEnv     = "CC_TEST_DATABASE_URL"
	containerName = "cc-cctest-v1"
	image         = "postgres:17.6-alpine"
	maxConns      = 4
	staleAfter    = 5 * time.Minute
	startTimeout  = 2 * time.Minute
	dropTimeout   = 30 * time.Second
	templateLock  = 7_051_998
)

var ErrNoServer = errors.New("no test Postgres: start Docker, or set " + serverEnv)

//go:embed entrypoint.sh
var entrypoint string

var testDatabase = regexp.MustCompile(`^cc_test_([0-9]+)_[0-9a-f]{8}$`)

type server struct {
	url      *url.URL
	admin    *pgxpool.Pool
	template string
}

var (
	once     sync.Once
	shared   *server
	startErr error
)

// DSN returns a migrated test database URL and drops it when tb finishes.
func DSN(tb testing.TB) string {
	tb.Helper()
	return dsnFor(tb, true)
}

// UnmigratedDSN provides a blank database for tests of the migration path itself.
func UnmigratedDSN(tb testing.TB) string {
	tb.Helper()
	return dsnFor(tb, false)
}

func dsnFor(tb testing.TB, cloned bool) string {
	tb.Helper()
	var dsn string
	var drop func() error
	var err error
	if cloned {
		dsn, drop, err = Create(tb.Context())
	} else {
		dsn, drop, err = create(tb.Context(), false)
	}
	if err != nil {
		tb.Fatalf("cctest: %v", err)
	}
	tb.Cleanup(func() {
		if err := drop(); err != nil {
			tb.Errorf("cctest: %v", err)
		}
	})
	return dsn
}

// Store opens a capped pool to a migrated test database and closes it before dropping the database.
func Store(tb testing.TB) *store.Store {
	tb.Helper()
	dsn := DSN(tb)
	st, err := store.OpenStore(dsn, store.MaxOpenConns(maxConns))
	if err != nil {
		tb.Fatalf("open test store: %v", err)
	}
	tb.Cleanup(func() {
		if err := st.Close(); err != nil {
			tb.Errorf("close test store: %v", err)
		}
	})
	return st
}

// Create returns a cloned database and a drop function for callers without a testing.TB.
func Create(ctx context.Context) (dsn string, drop func() error, err error) {
	return create(ctx, true)
}

func create(ctx context.Context, cloned bool) (dsn string, drop func() error, err error) {
	once.Do(func() {
		startup, cancel := context.WithTimeout(context.Background(), startTimeout)
		defer cancel()
		shared, startErr = start(startup)
	})
	if startErr != nil {
		return "", nil, startErr
	}
	name := fmt.Sprintf("cc_test_%d_%08x", time.Now().Unix(), rand.Uint32())
	statement := "CREATE DATABASE " + ident(name)
	if cloned {
		statement += " TEMPLATE " + ident(shared.template)
	}
	if _, err := shared.admin.Exec(ctx, statement); err != nil {
		return "", nil, fmt.Errorf("create database %s: %w", name, err)
	}
	address := shared.dsn(name)
	if cloned {
		u, err := url.Parse(address)
		if err != nil {
			return "", nil, fmt.Errorf("parse cloned database URL: %w", err)
		}
		q := u.Query()
		q.Set("application_name", "cc-cctest-clone")
		u.RawQuery = q.Encode()
		address = u.String()
	}
	return address, func() error {
		cleanup, cancel := context.WithTimeout(context.Background(), dropTimeout)
		defer cancel()
		if _, err := shared.admin.Exec(cleanup, "DROP DATABASE IF EXISTS "+ident(name)+" WITH (FORCE)"); err != nil {
			return fmt.Errorf("drop database %s: %w", name, err)
		}
		return nil
	}, nil
}

func start(ctx context.Context) (*server, error) {
	if raw := os.Getenv(serverEnv); raw != "" {
		s, err := connect(ctx, raw)
		if err != nil {
			return nil, errors.Join(ErrNoServer, err)
		}
		return s, nil
	}
	if err := os.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true"); err != nil {
		return nil, fmt.Errorf("disable ryuk: %w", errors.Join(ErrNoServer, err))
	}
	var errs []error
	for range 2 {
		s, err := runAndConnect(ctx)
		if err == nil {
			return s, nil
		}
		errs = append(errs, err)
	}
	return nil, fmt.Errorf("start postgres: %w", errors.Join(append([]error{ErrNoServer}, errs...)...))
}

func runAndConnect(ctx context.Context) (*server, error) {
	c, err := postgres.Run(ctx, image,
		testcontainers.WithReuseByName(containerName),
		testcontainers.WithEntrypoint("sh", "-c", entrypoint, "cctest"),
		testcontainers.WithEnv(map[string]string{"CCTEST_IDLE_SECONDS": strconv.Itoa(int(staleAfter.Seconds()))}),
		testcontainers.WithCmd("postgres", "-c", "fsync=off", "-c", "max_connections=500"),
		testcontainers.WithTmpfs(map[string]string{"/var/lib/postgresql/data": "rw"}),
		testcontainers.WithHostConfigModifier(func(hc *container.HostConfig) { hc.AutoRemove = true }),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		return nil, fmt.Errorf("run container %s: %w", containerName, err)
	}
	raw, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return nil, fmt.Errorf("container connection string: %w", err)
	}
	return connect(ctx, raw)
}

func connect(ctx context.Context, raw string) (*server, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return nil, fmt.Errorf("%s must be a postgres:// URL", serverEnv)
	}
	cfg, err := pgxpool.ParseConfig(raw)
	if err != nil {
		return nil, fmt.Errorf("parse server URL: %w", err)
	}
	cfg.MaxConns = 1
	admin, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect admin: %w", err)
	}
	s := &server{url: u, admin: admin}
	if err := s.prepare(ctx); err != nil {
		admin.Close()
		return nil, err
	}
	return s, nil
}

func (s *server) prepare(ctx context.Context) error {
	for {
		_, err := s.admin.Exec(ctx, "SELECT 1")
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("ping server: %w", err)
		case <-time.After(200 * time.Millisecond):
		}
	}
	if err := s.sweep(ctx, time.Now()); err != nil {
		return err
	}
	template, err := s.ensureTemplate(ctx)
	if err != nil {
		return err
	}
	s.template = template
	return nil
}

func (s *server) sweep(ctx context.Context, now time.Time) error {
	rows, err := s.admin.Query(ctx, `
		SELECT datname FROM pg_database d
		WHERE datname LIKE 'cc\_test\_%'
		AND NOT EXISTS (SELECT 1 FROM pg_stat_activity a WHERE a.datname = d.datname)
	`)
	if err != nil {
		return fmt.Errorf("list databases: %w", err)
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return fmt.Errorf("collect databases: %w", err)
	}
	for _, name := range names {
		match := testDatabase.FindStringSubmatch(name)
		if match == nil {
			continue
		}
		created, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil || now.Sub(time.Unix(created, 0)) < staleAfter {
			continue
		}
		if _, err := s.admin.Exec(ctx, "DROP DATABASE IF EXISTS "+ident(name)+" WITH (FORCE)"); err != nil {
			return fmt.Errorf("drop stale database %s: %w", name, err)
		}
	}
	return nil
}

func (s *server) ensureTemplate(ctx context.Context) (template string, err error) {
	hash, err := hashFS(store.Migrations())
	if err != nil {
		return "", err
	}
	template = "cc_template_" + hash
	conn, err := s.admin.Acquire(ctx)
	if err != nil {
		return "", fmt.Errorf("acquire admin connection: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", templateLock); err != nil {
		return "", fmt.Errorf("lock template: %w", err)
	}
	defer func() {
		_, unlockErr := conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", templateLock)
		if unlockErr != nil {
			err = errors.Join(err, fmt.Errorf("unlock template: %w", unlockErr))
		}
	}()
	var exists bool
	err = conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", template).Scan(&exists)
	if err != nil {
		return "", fmt.Errorf("find template: %w", err)
	}
	if exists {
		return template, nil
	}
	build := template + "_build"
	if _, err := conn.Exec(ctx, "DROP DATABASE IF EXISTS "+ident(build)+" WITH (FORCE)"); err != nil {
		return "", fmt.Errorf("drop stale template build: %w", err)
	}
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+ident(build)); err != nil {
		return "", fmt.Errorf("create template: %w", err)
	}
	st, err := store.OpenStore(s.dsn(build))
	if err != nil {
		return "", fmt.Errorf("migrate template: %w", err)
	}
	if err := st.Close(); err != nil {
		return "", fmt.Errorf("close template store: %w", err)
	}
	if _, err := conn.Exec(ctx, "ALTER DATABASE "+ident(build)+" RENAME TO "+ident(template)); err != nil {
		return "", fmt.Errorf("publish template: %w", err)
	}
	return template, nil
}

func (s *server) dsn(database string) string {
	u := *s.url
	u.Path = "/" + database
	return u.String()
}

func hashFS(fsys fs.FS) (string, error) {
	h := sha256.New()
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walk migration %s: %w", path, err)
		}
		if d.IsDir() {
			return nil
		}
		body, err := fs.ReadFile(fsys, path)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", path, err)
		}
		h.Write(fmt.Appendf(nil, "%s\x00%d\x00", path, len(body)))
		h.Write(body)
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("hash migrations: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil))[:12], nil
}

func ident(name string) string { return pgx.Identifier{name}.Sanitize() }
