package view

import (
	"fmt"
	"math"
	"strings"
)

type chartTick struct {
	Y     float64
	Label string
}

func polyline(count int, x, y func(i int) float64) string {
	var b strings.Builder
	for i := range count {
		cmd := "L"
		if i == 0 {
			cmd = "M"
		}
		fmt.Fprintf(&b, "%s %.2f %.2f ", cmd, x(i), y(i))
	}
	return strings.TrimSpace(b.String())
}

func niceTicks(maxValue float64, targetCount int) []float64 {
	if maxValue <= 0 {
		return []float64{0}
	}
	roughStep := maxValue / float64(targetCount)
	magnitude := math.Pow(10, math.Floor(math.Log10(roughStep)))
	residual := roughStep / magnitude
	rung := 10.0
	switch {
	case residual <= 1:
		rung = 1
	case residual <= 2:
		rung = 2
	case residual <= 5:
		rung = 5
	}
	step := rung * magnitude
	top := math.Ceil(maxValue/step) * step

	var values []float64
	for v := 0.0; v <= top+step/2; v += step {
		values = append(values, math.Round(v*1e6)/1e6)
	}
	return values
}
