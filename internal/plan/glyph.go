package plan

// Rail groups, in the order the session rail lists them.
const (
	GroupNeedsYou = "needs-you"
	GroupInFlight = "in-flight"
	GroupSettled  = "settled"
)

// RailGroup places a glyph word in a rail group. folded marks the in-flight glyphs the rail
// collapses behind a count.
func RailGroup(glyph GlyphWord) (group string, folded bool) {
	switch glyph {
	case GlyphFailed, GlyphAttention:
		return GroupNeedsYou, false
	case GlyphDone:
		return GroupSettled, false
	case GlyphReady, GlyphBlocked:
		return GroupInFlight, true
	case GlyphUnknown, GlyphRunning, GlyphPending, GlyphChecking:
		return GroupInFlight, false
	default:
		return GroupInFlight, false
	}
}
