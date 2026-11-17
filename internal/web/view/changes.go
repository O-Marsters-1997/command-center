package view

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strconv"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/adapter/git"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
)

var ErrNoLocalTip =errors.New("view: ticket has no local tip")

type ChangedFile struct {
	git.FileDiff
	Protected option.Option[plan.PathMatch]
}

type Changes struct {
	Ticket  plan.TicketURL
	Base    string
	Tip     plan.SHA
	State   plan.State
	PR      option.Option[string] // the PR's URL once one is open
	Files   []ChangedFile
	Added   int
	Deleted int
}

type ChangeFolder struct {
	Dir   string
	Files []ChangedFile
}

type ChangesPage struct {
	Chrome
	Changes
	SessionPath string
	ViewedKey   string
	Folders     []ChangeFolder
}

func (p ChangesPage) FileID(f ChangedFile) string {
	return "file-" + strconv.Itoa(slices.IndexFunc(p.Files, func(c ChangedFile) bool { return c.Path == f.Path }))
}

func (r *Reader) Changes(ctx context.Context, now time.Time, ref SessionRef, dataDir string) (ChangesPage, error) {
	board, err := r.Board(ctx, now, Params{})
	if err != nil {
		return ChangesPage{}, fmt.Errorf("load board: %w", err)
	}
	row, ok := board.rowFor(ref)
	if !ok {
		return ChangesPage{}, ErrSessionNotFound
	}
	in, err := r.store.PlanInput(ctx)
	if err != nil {
		return ChangesPage{}, fmt.Errorf("load plan input: %w", err)
	}
	in.Now = now
	snap := plan.RulesFor(plan.Daemon{Workflows: config.Workflows()}, in.Obs).Derive(in)
	entry, ok := snap.Entry(plan.TicketURL(row.URL))
	if !ok {
		return ChangesPage{}, ErrSessionNotFound
	}
	tip := in.Obs.LocalTips[entry.Ticket.Ref()]
	if tip == "" {
		return ChangesPage{}, ErrNoLocalTip
	}
	base := entry.Unlock.BaseBranch
	diffs, err := git.Diff(ctx, config.CheckoutPath(dataDir, row.Repo), "origin/"+base, string(tip))
	if err != nil {
		return ChangesPage{}, fmt.Errorf("diff %s against origin/%s: %w", entry.Ticket.Branch, base, err)
	}

	paths := make([]string, len(diffs))
	for i, d := range diffs {
		paths[i] = d.Path
	}
	settings := in.Obs.Settings[row.Repo]
	matches := plan.Classify(paths, plan.Policy{Deny: settings.Deny, Protect: settings.Protect})
	changes := Changes{
		Ticket: plan.TicketURL(row.URL), Base: base, Tip: tip, State: entry.State, PR: option.None[string](),
	}
	if row.PRNumber > 0 {
		changes.PR = option.Some("https://github.com/" + row.Repo + "/pull/" + strconv.Itoa(row.PRNumber))
	}
	for _, d := range diffs {
		file := ChangedFile{FileDiff: d, Protected: option.None[plan.PathMatch]()}
		if i := slices.IndexFunc(matches, func(m plan.PathMatch) bool { return m.Path == d.Path }); i >= 0 {
			file.Protected = option.Some(matches[i])
		}
		changes.Files = append(changes.Files, file)
		changes.Added += d.Added
		changes.Deleted += d.Deleted
	}

	self := ref.Path()
	chrome := board.Chrome
	chrome.Home = false
	chrome.titled("Changes · " + row.Ticket())
	crumbs := sessionCrumbs(row)
	crumbs[len(crumbs)-1].Path = self
	chrome.Crumbs = append(crumbs, Crumb{Label: "Changes"})
	return ChangesPage{
		Chrome:      chrome,
		Changes:     changes,
		SessionPath: self,
		ViewedKey:   "cc:viewed:" + row.URL + ":" + string(tip),
		Folders:     groupByFolder(changes.Files),
	}, nil
}

func groupByFolder(files []ChangedFile) []ChangeFolder {
	var folders []ChangeFolder
	for _, f := range files {
		dir := path.Dir(f.Path)
		i := slices.IndexFunc(folders, func(c ChangeFolder) bool { return c.Dir == dir })
		if i < 0 {
			folders = append(folders, ChangeFolder{Dir: dir})
			i = len(folders) - 1
		}
		folders[i].Files = append(folders[i].Files, f)
	}
	slices.SortStableFunc(folders, func(a, b ChangeFolder) int { return cmp.Compare(a.Dir, b.Dir) })
	return folders
}
