//go:build e2e

package main

import (
	"github.com/O-Marsters-1997/command-center/e2e/register"
	"github.com/O-Marsters-1997/command-center/internal/app"
)

func e2eCommands() map[string]command {
	cmds := map[string]command{}
	for name, cmd := range register.Commands() {
		cmds[name] = cmd
	}
	return cmds
}

func appSeams() app.Seams { return register.Seams() }
