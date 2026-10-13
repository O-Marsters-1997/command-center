//go:build e2e

package main

import (
	"github.com/O-Marsters-1997/command-center/e2e/register"
	"github.com/O-Marsters-1997/command-center/internal/app"
)

func subcmd(args []string) command {
	return register.Lookup(args)
}

func init() {
	runOptions = []app.Option{
		app.WithValidator(register.SandboxValidate), app.WithRemoteSource(register.SandboxRemote),
	}
}
