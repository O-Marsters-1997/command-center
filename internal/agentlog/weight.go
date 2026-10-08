package agentlog

import "strings"

type price struct {
	input, output, cacheRead, cacheCreate float64
}

// prices is keyed by the substring of a model id that names its tier, so a dated id
// ("claude-sonnet-5-20260101") still matches. sonnet is solved from Anthropic's own reported
// total_cost_usd for run27 (weight_test.go) on Sonnet 5; claude-sonnet-5-5 reuses that rate
// uncalibrated. opus and haiku are extrapolated, uncalibrated.
var prices = map[string]price{
	"sonnet": {input: 6.40e-6, output: 32.00e-6, cacheRead: 0.64e-6, cacheCreate: 12.80e-6},
	"opus":   {input: 19.20e-6, output: 96.00e-6, cacheRead: 1.92e-6, cacheCreate: 38.40e-6},
	"haiku":  {input: 1.28e-6, output: 6.40e-6, cacheRead: 0.128e-6, cacheCreate: 2.56e-6},
}

const defaultTier = "sonnet"

func priceFor(model string) price {
	for tier, p := range prices {
		if strings.Contains(model, tier) {
			return p
		}
	}
	return prices[defaultTier]
}

// Weight prices one RequestUsage in dollars, by its model's per-token API rate.
func Weight(u RequestUsage) float64 {
	p := priceFor(u.Model)
	return float64(u.Input)*p.input + float64(u.Output)*p.output +
		float64(u.CacheRead)*p.cacheRead + float64(u.CacheCreate)*p.cacheCreate
}
