//go:build e2e

package main

import (
	"context"

	"github.com/O-Marsters-1997/command-center/e2e/register"
	"github.com/O-Marsters-1997/command-center/internal/app"
)

func subcmd(args []string) func(ctx context.Context, configPath string) error {
	return register.Lookup(args)
}

func init() {
	runOptions = []app.Option{app.WithCheckout(register.SandboxCheckout)}
}
