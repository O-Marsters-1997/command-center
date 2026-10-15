//go:build e2e

package main

import (
	"context"
	"fmt"

	"github.com/O-Marsters-1997/command-center/e2e/register"
	"github.com/O-Marsters-1997/command-center/internal/app"
)

func openCommand(ctx context.Context, deps app.Deps) error {
	return run(ctx, deps)
}

func e2eCommand(ctx context.Context, deps app.Deps, args []string) error {
	if err := register.Run(ctx, deps, args); err != nil {
		return fmt.Errorf("run e2e command: %w", err)
	}
	return nil
}

func newApp(ctx context.Context, deps app.Deps) (*app.App, error) {
	deps.Validate = register.SandboxValidate
	deps.RemoteOf = register.SandboxRemote
	instance, err := app.New(ctx, deps)
	if err != nil {
		return nil, fmt.Errorf("create e2e app: %w", err)
	}
	return instance, nil
}
