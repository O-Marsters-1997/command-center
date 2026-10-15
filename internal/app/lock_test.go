package app_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/app"
)

func TestLock(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "command-centre.lock")
	held, err := app.Lock(path)
	if err != nil {
		t.Fatalf("Lock(%q) error = %v", path, err)
	}
	t.Cleanup(func() {
		if err := held.Close(); err != nil {
			t.Errorf("release lock: %v", err)
		}
	})

	if _, err := app.Lock(path); !errors.Is(err, app.ErrLocked) {
		t.Errorf("Lock(%q) held = %v, want %v", path, err, app.ErrLocked)
	}
}
