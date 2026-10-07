package view

import (
	"fmt"
	"slices"
	"strconv"
)

const (
	insightsChartWidth     = 720.0
	insightsChartHeight    = 220.0
	insightsChartPadLeft   = 52.0
	insightsChartPadTop    = 8.0
	insightsChartPadBottom = 20.0
	insightsMedianWindow   = 10
)

type insightsDot struct {
	X, Y  float64
	Title string
}

type InsightsChart struct {
	Width, Height float64
	PadLeft       float64
	Count         int
	Ticks         []chartTick
	MedianPath    string
	MedianWindow  int
	Dots          []insightsDot
	HasWaste      bool
	WasteY        float64
	WasteTitle    string
	WasteLabel    string
}

func buildInsightsChart(resp InsightsResponse) InsightsChart {
	if len(resp.Points) == 0 {
		return InsightsChart{}
	}

	pcts := make([]float64, len(resp.Points))
	maxPct := max(0, resp.WastePctWeek)
	for i, p := range resp.Points {
		pcts[i] = p.PctWeek
		maxPct = max(maxPct, p.PctWeek)
	}
	tickValues := niceTicks(maxPct, 4)
	top := tickValues[len(tickValues)-1]
	if top == 0 {
		top = 1
	}

	plotWidth := insightsChartWidth - insightsChartPadLeft
	plotHeight := insightsChartHeight - insightsChartPadTop - insightsChartPadBottom
	slotWidth := plotWidth / float64(len(pcts))
	scaleX := func(i int) float64 { return insightsChartPadLeft + (float64(i)+0.5)*slotWidth }
	scaleY := func(v float64) float64 { return insightsChartPadTop + plotHeight - v/top*plotHeight }

	chart := InsightsChart{
		Width: insightsChartWidth, Height: insightsChartHeight, PadLeft: insightsChartPadLeft,
		Count: len(pcts), MedianWindow: insightsMedianWindow,
		Ticks: make([]chartTick, len(tickValues)),
		Dots:  make([]insightsDot, len(pcts)),
	}
	for i, v := range tickValues {
		chart.Ticks[i] = chartTick{Y: scaleY(v), Label: strconv.FormatFloat(v, 'f', -1, 64) + "%"}
	}

	medians := rollingMedian(pcts, insightsMedianWindow)
	chart.MedianPath = polyline(len(medians), scaleX, func(i int) float64 { return scaleY(medians[i]) })

	for i, p := range resp.Points {
		name := p.Title
		if name == "" {
			name = p.Ticket
		}
		chart.Dots[i] = insightsDot{
			X: scaleX(i), Y: scaleY(p.PctWeek),
			Title: fmt.Sprintf("%s: %.2f%% week (agent %.2f, resolve %.2f, follow-up %.2f), merged %s",
				name, p.PctWeek, p.AgentPctWeek, p.ResolvePctWeek, p.FollowUpPctWeek, p.MergedAt),
		}
	}

	if resp.WastePctWeek > 0 {
		chart.HasWaste = true
		chart.WasteY = scaleY(resp.WastePctWeek)
		chart.WasteLabel = fmt.Sprintf("%.2f%% week", resp.WastePctWeek)
		chart.WasteTitle = "waste (withdrawn tickets): " + chart.WasteLabel
	}
	return chart
}

func rollingMedian(values []float64, window int) []float64 {
	medians := make([]float64, len(values))
	for i := range values {
		slice := slices.Clone(values[max(0, i-window+1) : i+1])
		slices.Sort(slice)
		mid := len(slice) / 2
		if len(slice)%2 == 0 {
			medians[i] = (slice[mid-1] + slice[mid]) / 2
		} else {
			medians[i] = slice[mid]
		}
	}
	return medians
}
