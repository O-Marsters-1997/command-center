package view

type KeyRow struct {
	Keys string
	Does string
}

var Keymap = []KeyRow{
	{"⌘K", "Search sessions, refs, repos and features"},
	{"J / K", "Next / previous session in the rail"},
	{"G H · G T · G I · G R", "Home · All tickets · Insights · Repos"},
	{"G 1 – G 9", "Scope to project 1–9"},
	{"G 0", "All projects"},
	{"C", "Launch a feature"},
	{"F · V", "Open the feature graph · switch graph and board"},
	{"E", "Settle a merged session (removes its worktree)"},
	{"R", "Re-run"},
	{"O", "Open the PR"},
	{"Enter", "Focus the composer"},
	{"Esc", "Close the sheet; clear the search"},
	{"?", "Show this sheet"},
}
