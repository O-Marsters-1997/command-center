package plan

// SeamCheck is one row's inputs to SeamChanged.
type SeamCheck struct {
	HasRun       bool
	Authorised   bool
	ComposeOK    bool
	ComposedHash string
	RunHash      string
	MemberHash   string
}

// SeamChanged reports whether c.ComposedHash has drifted from the hash the row is bound to: the
// latest run's own hash once HasRun, else the active launch membership's hash. A row that is
// neither has consented to nothing, so it never changes.
func SeamChanged(c SeamCheck) bool {
	switch {
	case c.HasRun:
		return !c.ComposeOK || c.ComposedHash != c.RunHash
	case c.Authorised:
		return !c.ComposeOK || c.ComposedHash != c.MemberHash
	default:
		return false
	}
}
