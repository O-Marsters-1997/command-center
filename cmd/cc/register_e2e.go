//go:build e2e

package main

import (
	"context"

	"github.com/O-Marsters-1997/command-center/e2e/register"
	"github.com/O-Marsters-1997/command-center/internal/cc"
)

// subcmd resolves the end-to-end-only subcommands. The build tag, not an init hook, is what
// keeps them out of the release binary.
func subcmd(args []string) func(ctx context.Context, configPath string) error {
	return register.Lookup(args)
}

func init() {
	runOptions = []cc.Option{cc.WithCheckout(register.SandboxCheckout)}
}
