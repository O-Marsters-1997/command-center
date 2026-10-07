package view

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

const (
	curveWidth     = 320.0
	curveHeight    = 96.0
	curvePadLeft   = 32.0
	curvePadTop    = 8.0
	curvePadBottom = 16.0
)

type curveSeries struct {
	Label string
	Class string
	Path  string
	Title string
}

type chartTick struct {
	Y     float64
	Label string
}

type ContextCurve struct {
	Series []curveSeries
	Ticks  []chartTick
	Width  float64
	Height float64
}

type curvePoint struct {
	x   int
	ctx int64
}

func buildContextCurve(requests []store.RunRequest) ContextCurve {
	if len(requests) == 0 {
		return ContextCurve{}
	}

	var order []string
	byThread := map[string][]curvePoint{}
	var maxContext int64
	for i, r := range requests {
		context := r.InputTokens + r.CacheCreationTokens + r.CacheReadTokens
		if _, ok := byThread[r.Thread]; !ok {
			order = append(order, r.Thread)
		}
		byThread[r.Thread] = append(byThread[r.Thread], curvePoint{x: i, ctx: context})
		maxContext = max(maxContext, context)
	}

	plotWidth := curveWidth - curvePadLeft
	plotHeight := curveHeight - curvePadTop - curvePadBottom
	lastIndex := len(requests) - 1
	scaleX := func(i int) float64 {
		if lastIndex <= 0 {
			return curvePadLeft
		}
		return curvePadLeft + float64(i)/float64(lastIndex)*plotWidth
	}
	scaleY := func(v int64) float64 {
		if maxContext == 0 {
			return curvePadTop + plotHeight
		}
		return curvePadTop + plotHeight - float64(v)/float64(maxContext)*plotHeight
	}

	subagents := 0
	series := make([]curveSeries, len(order))
	for i, thread := range order {
		label := "main"
		if thread != agentlog.MainThread {
			subagents++
			label = fmt.Sprintf("subagent %d", subagents)
		}
		points := byThread[thread]
		values := make([]int64, len(points))
		for j, p := range points {
			values[j] = p.ctx
		}
		series[i] = curveSeries{
			Label: label,
			Class: fmt.Sprintf("chart-series-%d", i%5),
			Path: polyline(len(points),
				func(j int) float64 { return scaleX(points[j].x) },
				func(j int) float64 { return scaleY(points[j].ctx) }),
			Title: label + ": " + joinInt64s(values),
		}
	}

	return ContextCurve{
		Series: series,
		Ticks:  contextCurveTicks(maxContext, scaleY),
		Width:  curveWidth,
		Height: curveHeight,
	}
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

func contextCurveTicks(maxContext int64, scaleY func(int64) float64) []chartTick {
	values := niceTicks(float64(maxContext), 4)
	ticks := make([]chartTick, len(values))
	for i, v := range values {
		tokens := int64(math.Round(v))
		ticks[i] = chartTick{Y: scaleY(tokens), Label: strconv.FormatInt(tokens, 10)}
	}
	return ticks
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

func joinInt64s(values []int64) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = strconv.FormatInt(v, 10)
	}
	return strings.Join(parts, ", ")
}
