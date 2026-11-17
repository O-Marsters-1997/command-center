package gh

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/adapter/command"
)

const (
	readAttempts   = 3
	defaultBackoff = 500 * time.Millisecond
)

type CLI struct {
	Exec    command.Executor
	Backoff time.Duration
}

func (c CLI) Read(ctx context.Context, dir string, args ...string) ([]byte, error) {
	wait := cmp.Or(c.Backoff, defaultBackoff)
	for attempt := 1; ; attempt++ {
		out, err := c.Run(ctx, dir, args...)
		if err == nil || attempt == readAttempts || !isNetworkTimeout(err) {
			return out, err
		}
		select {
		case <-ctx.Done():
			return nil, errors.Join(err, ctx.Err())
		case <-time.After(wait):
		}
		wait *= 2
	}
}

func isNetworkTimeout(err error) bool {
	var cmdErr *command.Error
	if !errors.As(err, &cmdErr) {
		return false
	}
	return strings.Contains(cmdErr.Stderr, "operation timed out") || strings.Contains(cmdErr.Stderr, "i/o timeout")
}

func (c CLI) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	out, err := command.OrExec(c.Exec).Output(ctx, command.DefaultTimeout, dir, "gh", args...)
	if err != nil {
		return nil, fmt.Errorf("run gh: %w", err)
	}
	return out, nil
}

func Decode[T any](raw []byte) (T, error) {
	var decoded T
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return decoded, fmt.Errorf("decode %T: %w", decoded, err)
	}
	return decoded, nil
}

func DecodeStream[T any](raw []byte) ([]T, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	var all []T
	for pages := 0; ; pages++ {
		var page []T
		err := dec.Decode(&page)
		if errors.Is(err, io.EOF) && pages > 0 {
			return all, nil
		}
		if err != nil {
			return nil, fmt.Errorf("decode %T pages: %w", page, err)
		}
		all = append(all, page...)
	}
}
