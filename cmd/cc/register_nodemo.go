//go:build !demo

package main

import (
	"context"

	"github.com/O-Marsters-1997/command-center/internal/app"
)

func demoCommand(ctx context.Context, deps app.Deps) error {
	return run(ctx, deps)
}
