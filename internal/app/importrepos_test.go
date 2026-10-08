package app_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/app"
	"github.com/O-Marsters-1997/command-center/internal/cctest"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

const legacyConfig = "port = 0\n[[repo]]\nname = \"command-center\"\n" +
	"remote = \"git@github.com:O-Marsters-1997/command-center.git\"\n"

var skipCheckout = app.WithCheckout(func(context.Context, string, []store.Repo) error { return nil })

func TestFirstBootImportsRepoBlocksAndASecondBootRefusesThem(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("CC_DATA_DIR", dataDir)
	dsn := cctest.DSN(t)
	t.Setenv("CC_DATABASE_URL", dsn)

	legacyCheckout := filepath.Join(dataDir, "repos", "command-center")
	if err := os.MkdirAll(legacyCheckout, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyCheckout, "README.md"), []byte("hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	seed(t, dsn, func(db *store.Store) error {
		return db.UpsertTickets(t.Context(), []store.Ticket{
			{URL: "https://github.com/O-Marsters-1997/command-center/issues/1", Repo: "command-center", Branch: "issue-1"},
		})
	})
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(legacyConfig), 0o600); err != nil {
		t.Fatal(err)
	}

	first, err := app.New(t.Context(), configPath, skipCheckout, stubSquashOnly)
	if err != nil {
		t.Fatalf("first boot: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	const fullName = "O-Marsters-1997/command-center"
	seed(t, dsn, func(db *store.Store) error {
		repos, err := db.Repos(t.Context())
		if err != nil {
			return err
		}
		if len(repos) != 1 || repos[0].Name != fullName || repos[0].State != store.RepoReady {
			t.Errorf("repos after first boot = %+v, want one ready %s", repos, fullName)
		}
		tickets, err := db.Tickets(t.Context())
		if err != nil {
			return err
		}
		if len(tickets) != 1 || tickets[0].Repo != fullName {
			t.Errorf("tickets after first boot = %+v, want repo %s", tickets, fullName)
		}
		return nil
	})

	moved := config.CheckoutPath(dataDir, fullName)
	if _, err := os.Stat(filepath.Join(moved, "README.md")); err != nil {
		t.Errorf("checkout was not moved to %s: %v", moved, err)
	}
	if info, err := os.Lstat(legacyCheckout); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("%s is not a symlink after the move: %v", legacyCheckout, err)
	}
	if _, err := os.Stat(filepath.Join(legacyCheckout, "README.md")); err != nil {
		t.Errorf("the old checkout path no longer resolves: %v", err)
	}

	_, err = app.New(t.Context(), configPath, skipCheckout, stubSquashOnly)
	if err == nil || !strings.Contains(err.Error(), "still has [[repo]] blocks") {
		t.Errorf("second boot with [[repo]] = %v, want a refusal telling the operator to delete them", err)
	}
}
