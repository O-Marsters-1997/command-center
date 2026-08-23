package cc_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/cc"
)

func TestLockRefusesASecondInstance(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "cc.db")
	held, err := cc.Lock(path)
	if err != nil {
		t.Fatalf("first Lock: %v", err)
	}
	t.Cleanup(func() {
		if err := held.Close(); err != nil {
			t.Errorf("release lock: %v", err)
		}
	})

	_, err = cc.Lock(path)
	if err == nil {
		t.Fatal("second Lock succeeded; want a refusal")
	}
	if !strings.Contains(err.Error(), "lock") || !strings.Contains(err.Error(), path) {
		t.Errorf("error %q names neither the lock nor the path", err)
	}
}
