//go:build e2e

package main

import (
	"github.com/O-Marsters-1997/command-center/e2e/register"
)

func init() {
	seams = register.Seams()
	for name, cmd := range register.Commands() {
		subcommands[name] = cmd
	}
}
