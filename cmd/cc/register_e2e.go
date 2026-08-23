//go:build e2e

package main

import (
	"context"

	"github.com/O-Marsters-1997/command-center/e2e/register"
)

// subcmd resolves the end-to-end-only subcommands. The build tag, not an init hook, is what
// keeps them out of the release binary.
func subcmd(args []string) func(ctx context.Context, configPath string) error {
	return register.Lookup(args)
}
