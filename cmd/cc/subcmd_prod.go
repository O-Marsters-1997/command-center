//go:build !e2e

package main

import "github.com/O-Marsters-1997/command-center/internal/app"

func e2eCommands() map[string]command { return nil }

func appSeams() app.Seams { return app.Seams{} }
