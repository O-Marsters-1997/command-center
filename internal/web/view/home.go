package view

import (
	"context"
	"fmt"
	"time"
)

const ticketsPath = "/tickets"

type Home struct {
	Chrome
	Target string
}

func (r *Reader) Home(ctx context.Context, now time.Time) (Home, error) {
	chrome, err := r.Chrome(ctx, now, Params{})
	if err != nil {
		return Home{}, fmt.Errorf("load chrome: %w", err)
	}
	rail, err := r.Rail(ctx, now, RailParams{})
	if err != nil {
		return Home{}, fmt.Errorf("load rail: %w", err)
	}
	chrome.Home = true
	chrome.Section = "tickets"
	home := Home{Chrome: chrome, Target: ticketsPath}
	if len(rail.NeedsYou) > 0 {
		home.Target = rail.NeedsYou[0].Path
	}
	return home, nil
}
