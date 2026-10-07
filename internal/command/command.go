package command

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

func Output(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		return out, fmt.Errorf("%s %s in %s: %w: %s",
			name, strings.Join(args, " "), dir, err, bytes.TrimSpace(stderr.Bytes()))
	}
	return out, nil
}
