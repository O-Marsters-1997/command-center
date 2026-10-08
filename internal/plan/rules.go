package plan

import "github.com/O-Marsters-1997/command-center/internal/verdict"

type Rules struct {
	Stacking     map[string]bool
	Deny         map[string][]string
	Checks       map[string]verdict.Predicate
	MergifySHA   map[string]string
	CompatCheck  map[string]string
	MaxAgents    int
	SpendLimit5h int
}
