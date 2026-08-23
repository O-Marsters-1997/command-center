//go:build unix

package cc

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// Flock is an exclusive advisory lock on a path, held for the life of the process (inv. 9:
// one app instance per workspace).
type Flock struct {
	f *os.File
}

// Lock takes an exclusive non-blocking flock on path, creating the file if needed.
// The lock is taken on the DB path itself, so a second instance is refused before it opens
// SQLite and before it can write anything.
func Lock(path string) (*Flock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, errors.Join(
			fmt.Errorf("cannot take the flock on %s: another command centre is already running "+
				"against this workspace: %w", path, err),
			f.Close())
	}
	return &Flock{f: f}, nil
}

func (l *Flock) Close() error {
	if err := syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN); err != nil {
		return errors.Join(fmt.Errorf("release flock: %w", err), l.f.Close())
	}
	return l.f.Close()
}
