//go:build !e2e

package main

import "context"

// subcmd reports that the release binary has no subcommands: its surface is the page plus the
// loop. `cc tick` and `cc request` exist only under -tags=e2e.
func subcmd([]string) func(ctx context.Context, configPath string) error { return nil }
