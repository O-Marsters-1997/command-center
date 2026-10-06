package spend

// Pct is a utilization fraction as a whole percentage, rounded half up.
func Pct(utilization float64) int { return int(utilization*100 + 0.5) }

// Paused reports whether the five-hour window's latest utilization has reached limit5h percent.
// A limit of zero or less never pauses.
func Paused(fiveHourUtilization float64, limit5h int) bool {
	return limit5h > 0 && Pct(fiveHourUtilization) >= limit5h
}

// Share is cc's own share of a window already totalPct percent used: its recorded dollars scaled
// by the window's fit, clamped inside [0, totalPct]. calibrating is true, and the share zero,
// when fit is the zero Result or below MinSamples.
func Share(totalPct int, fit Result, ccUSD float64) (pct int, calibrating bool) {
	if fit.Samples < MinSamples {
		return 0, true
	}
	return max(0, min(Pct(ccUSD*fit.Factor), totalPct)), false
}

// PctWeek is usd as a percentage of the weekly window under factor.
func PctWeek(usd, factor float64) float64 { return usd * factor * 100 }

// KindPctWeek is each spend kind's weekly percentage under factor, and their sum.
func KindPctWeek(agentUSD, resolveUSD, followUpUSD, factor float64) (agent, resolve, followUp, total float64) {
	agent = PctWeek(agentUSD, factor)
	resolve = PctWeek(resolveUSD, factor)
	followUp = PctWeek(followUpUSD, factor)
	return agent, resolve, followUp, agent + resolve + followUp
}
