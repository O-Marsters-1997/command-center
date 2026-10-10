package plan

// The eight glyph words, the only colour key the surface knows.
const (
	GlyphFailed    = "failed"
	GlyphAttention = "attention"
	GlyphRunning   = "running"
	GlyphPending   = "pending"
	GlyphChecking  = "checking"
	GlyphReady     = "ready"
	GlyphBlocked   = "blocked"
	GlyphDone      = "done"
)

// Rail groups, in the order the session rail lists them.
const (
	GroupNeedsYou = "needs-you"
	GroupInFlight = "in-flight"
	GroupSettled  = "settled"
)

// Glyph is the state's status glyph: one of the eight Glyph words, never a utility class.
func Glyph(s State) string {
	switch s {
	case Failed, CutFailed, PushFailed, CIFailed, ConflictsWithMain, RefreshConflicted,
		BaseGone, VerificationFailed, PRClosedUnmerged:
		return GlyphFailed
	case ReviewMe, NeedsYou, ConflictResolved:
		return GlyphAttention
	case Running:
		return GlyphRunning
	case PushPending, Queued, BaseMoved:
		return GlyphPending
	case Checking:
		return GlyphChecking
	case Ready, Cancelled:
		return GlyphReady
	case Blocked, WaitingOnProducerDeploy:
		return GlyphBlocked
	case PRMerged:
		return GlyphDone
	default:
		return GlyphFailed
	}
}

// RailGroup places a glyph word in a rail group. folded marks the in-flight glyphs the rail
// collapses behind a count.
func RailGroup(glyph string) (group string, folded bool) {
	switch glyph {
	case GlyphFailed, GlyphAttention:
		return GroupNeedsYou, false
	case GlyphDone:
		return GroupSettled, false
	case GlyphReady, GlyphBlocked:
		return GroupInFlight, true
	default:
		return GroupInFlight, false
	}
}
