package plan

import "github.com/O-Marsters-1997/command-center/internal/verdict"

// Rules is every per-repo and global setting a decision reads, built once from Config. The
// per-repo maps are keyed by repo name; a repo that never opted in has the zero value.
type Rules struct {
	Stacking     map[string]bool
	Deny         map[string][]string
	Checks       map[string]verdict.Predicate
	MergifySHA   map[string]string
	CompatCheck  map[string]string
	MaxAgents    int
	SpendLimit5h int
}
