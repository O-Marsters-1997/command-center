//go:build !e2e

package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/auth"
	"github.com/O-Marsters-1997/command-center/internal/cctest"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

func TestUseraddAndPasswdPrintAPasswordThatVerifies(t *testing.T) {
	dsn := cctest.DSN(t)
	configPath := filepath.Join(t.TempDir(), "config.toml")
	body := fmt.Sprintf("database_url = %q\ndata_dir = %q\n", dsn, t.TempDir())
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()

	created := captureStdout(t, func() {
		if err := useradd(ctx, configPath, []string{"olly@example.com"}); err != nil {
			t.Fatalf("useradd: %v", err)
		}
	})
	if !verifies(t, dsn, "olly@example.com", created) {
		t.Errorf("useradd printed %q, which does not verify against the stored hash", created)
	}

	changed := captureStdout(t, func() {
		if err := passwd(ctx, configPath, []string{"olly@example.com"}); err != nil {
			t.Fatalf("passwd: %v", err)
		}
	})
	if changed == created || !verifies(t, dsn, "olly@example.com", changed) {
		t.Errorf("passwd printed %q after useradd printed %q, want a new password that verifies", changed, created)
	}

	if err := useradd(ctx, configPath, nil); err == nil {
		t.Error("useradd with no email = nil error, want usage")
	}
	if err := passwd(ctx, configPath, []string{"nobody@example.com"}); err == nil {
		t.Error("passwd for an unknown email = nil error, want a failure")
	}
	if err := useradd(ctx, configPath, []string{"olly@example.com"}); err == nil {
		t.Error("useradd for an existing email = nil error, want a failure")
	}
}

func verifies(t *testing.T, dsn, email, password string) bool {
	t.Helper()
	st, err := store.OpenStore(dsn)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer st.Close()
	row, err := st.UserForLogin(t.Context(), email)
	if err != nil {
		t.Fatalf("UserForLogin: %v", err)
	}
	return auth.VerifyPassword(password, row.PasswordHash)
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	func() {
		defer func() { os.Stdout = orig }()
		fn()
	}()
	w.Close()
	t.Cleanup(func() { r.Close() })
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}
