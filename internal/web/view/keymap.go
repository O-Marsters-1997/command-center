package view

import "github.com/O-Marsters-1997/command-center/internal/plan"

type KeyRow struct {
	Keys string
	Does string
}

type KeyGroup struct {
	Title string
	Rows  []KeyRow
}

var Keymap = []KeyGroup{
	{"Anywhere", []KeyRow{
		{"⌘K", "Search sessions, refs, repos and features"},
		{"J / K", "Next / previous session in the rail"},
		{"G H · G T · G I · G R", "Home · All tickets · Insights · Repos"},
		{"G 1 – G 9", "Scope to project 1–9"},
		{"G 0", "All projects"},
		{"C", "Launch a feature"},
		{"?", "Show this sheet"},
		{"Esc", "Close the sheet; clear the search"},
	}},
	{"In a session", []KeyRow{
		{"F", "Open the feature graph"},
		verbKeyRow(plan.VerbRemoveWorktree, "Settle a merged session (removes its worktree)"),
		verbKeyRow(plan.VerbReRun, "Re-run"),
		{"O", "Open the PR"},
		{"Enter", "Focus the composer"},
	}},
	{"In a feature", []KeyRow{
		{"V", "Switch graph and board"},
	}},
}
