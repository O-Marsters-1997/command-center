//go:build !e2e

package main

import (
	"github.com/O-Marsters-1997/command-center/internal/app"
)

func subcmd(args []string) command { return openSubcmd(args) }

func appOptions() []app.Option { return nil }
