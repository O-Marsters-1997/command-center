package gh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/O-Marsters-1997/command-center/internal/adapter/command"
)

type CLI struct {
	Exec command.Executor
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
