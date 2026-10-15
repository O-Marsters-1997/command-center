package view

import (
	"context"
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

type frame struct {
	now      time.Time
	params   Params
	tickets  []store.Ticket
	in       plan.Input
	snap     plan.Snapshot
	repos    []store.Repo
	projects map[string]ProjectMark
	lastErr  option.Option[store.TickError]
	gauges   map[agentlog.Window]store.Gauge
	chrome   Chrome

	project     store.Project
	projectList []store.Project
	split       map[agentlog.Window]windowSplit
}

func (r *Reader) loadFrame(ctx context.Context, now time.Time, p Params) (frame, error) {
	f := frame{now: now}
	var err error
	if f.tickets, err = r.store.Tickets(ctx); err != nil {
		return frame{}, fmt.Errorf("load tickets: %w", err)
	}
	if f.in, err = r.store.PlanInput(ctx); err != nil {
		return frame{}, fmt.Errorf("load plan input: %w", err)
	}
	if f.repos, err = r.store.Repos(ctx); err != nil {
		return frame{}, fmt.Errorf("load repos: %w", err)
	}
	projects, err := r.store.Projects(ctx)
	if err != nil {
		return frame{}, fmt.Errorf("load projects: %w", err)
	}
	f.projectList = projects
	if f.lastErr, err = r.store.LastError(ctx); err != nil {
		return frame{}, fmt.Errorf("load last error: %w", err)
	}
	if f.gauges, err = r.store.LatestReadings(ctx); err != nil {
		return frame{}, fmt.Errorf("load latest readings: %w", err)
	}
	if f.split, err = r.gaugeSplit(ctx, now); err != nil {
		return frame{}, fmt.Errorf("gauge split: %w", err)
	}
	p.Repo = normalizeRepoScope(p.Repo, f.repos)
	f.project = normalizeProjectScope(p.Project, projects)
	p.Project = f.project.Name
	p.Feature = normalizeFeatureScope(p.Feature, distinctFeatures(f.tickets))
	f.params = p
	f.in.Now = now
	f.snap = plan.RulesFor(plan.Daemon{}, f.in.Obs).Derive(f.in)
	f.projects = projectMarks(f.repos)
	f.chrome = r.buildChrome(chromeInput{
		tickets: f.tickets, obs: f.in.Obs, observed: f.in.Observed,
		lastErr: f.lastErr, gauges: f.gauges, split: f.split,
	}, now, p)
	f.chrome.scopeToProject(f.project)
	f.chrome.ScopeMenu = scopeChoices(ScopeList(projects, f.repos), f.project.Name, p.Repo)
	f.chrome.RefusedRepos = refusedRepos(f.repos)
	rows := deriveRows(f.tickets, f.in, f.snap, nil)
	rows = scopeRailRows(rows, RailParams{
		Project: f.project.Name, Repo: p.Repo, projectRepos: f.project.Repos,
	})
	for _, row := range rows {
		if row.Glyph != plan.GlyphDone {
			f.chrome.OpenTickets++
		}
	}
	f.chrome.badge(keyTickets, fmt.Sprint(f.chrome.OpenTickets), false)
	if n := len(f.chrome.RefusedRepos); n > 0 {
		f.chrome.badge(keyRepos, fmt.Sprintf("%d refused", n), true)
	}
	return f, nil
}
