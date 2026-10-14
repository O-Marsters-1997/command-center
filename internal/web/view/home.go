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
	f, err := r.loadFrame(ctx, now, Params{})
	if err != nil {
		return Home{}, fmt.Errorf("load frame: %w", err)
	}
	chrome := f.chrome
	rail := railFromFrame(f, RailParams{})
	chrome.Home = true
	chrome.Section = "tickets"
	home := Home{Chrome: chrome, Target: ticketsPath}
	if len(rail.NeedsYou) > 0 {
		home.Target = rail.NeedsYou[0].Path
	}
	return home, nil
}
