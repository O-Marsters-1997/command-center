package plan

const (
	GroupNeedsYou = "needs-you"
	GroupInFlight = "in-flight"
	GroupSettled  = "settled"
)

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
