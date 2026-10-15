//go:build unix

package app

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// Flock is an exclusive advisory lock on a path, held for the life of the process.
type Flock struct {
	f *os.File
}

// ErrLocked indicates that another process holds the state lock.
var ErrLocked = errors.New("another cc holds the state lock")

// Lock takes an exclusive non-blocking flock on path, creating the file if needed.
// A second holder is refused with an error naming the workspace.
func Lock(path string) (*Flock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			err = fmt.Errorf("lock %s: %w", path, ErrLocked)
		} else {
			err = fmt.Errorf("take lock %s: %w", path, err)
		}
		return nil, errors.Join(err, f.Close())
	}
	return &Flock{f: f}, nil
}

func (l *Flock) Close() error {
	if err := syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN); err != nil {
		return errors.Join(fmt.Errorf("release flock: %w", err), l.f.Close())
	}
	if err := l.f.Close(); err != nil {
		return fmt.Errorf("close lock file: %w", err)
	}
	return nil
}
