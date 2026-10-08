package spend

func Pct(utilization float64) int { return int(utilization*100 + 0.5) }

func Paused(fiveHourUtilization float64, limit5h int) bool {
	return limit5h > 0 && Pct(fiveHourUtilization) >= limit5h
}

// Share is cc's own share of a window already totalPct percent used, clamped inside
// [0, totalPct]. calibrating is true, and the share zero, when fit is below MinSamples.
func Share(totalPct int, fit Result, ccUSD float64) (pct int, calibrating bool) {
	if fit.Samples < MinSamples {
		return 0, true
	}
	return max(0, min(Pct(ccUSD*fit.Factor), totalPct)), false
}

func PctWeek(usd, factor float64) float64 { return usd * factor * 100 }

func KindPctWeek(agentUSD, resolveUSD, followUpUSD, factor float64) (agent, resolve, followUp, total float64) {
	agent = PctWeek(agentUSD, factor)
	resolve = PctWeek(resolveUSD, factor)
	followUp = PctWeek(followUpUSD, factor)
	return agent, resolve, followUp, agent + resolve + followUp
}
