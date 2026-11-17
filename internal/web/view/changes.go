package view

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/adapter/git"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
)

var ErrNoLocalTip = errors.New("view: ticket has no local tip")

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

	Comments map[string][]plan.ReviewComment // by path

	CanFollowUp bool
}

func (c Changes) Pending() int {
	n := 0
	for _, cs := range c.Comments {
		n += len(cs)
	}
	return n
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
	Reason      plan.Reason
}

func (p ChangesPage) FileID(f ChangedFile) string {
	return "file-" + strconv.Itoa(slices.IndexFunc(p.Files, func(c ChangedFile) bool { return c.Path == f.Path }))
}

type changeSet struct {
	board Board
	row   Row
	in    plan.Input
	entry plan.Entry
	tip   plan.SHA
	base  string
	diffs []git.FileDiff
}

func (r *Reader) loadChanges(ctx context.Context, now time.Time, ref SessionRef, dataDir string) (changeSet, error) {
	board, err := r.Board(ctx, now, Params{})
	if err != nil {
		return changeSet{}, fmt.Errorf("load board: %w", err)
	}
	row, ok := board.rowFor(ref)
	if !ok {
		return changeSet{}, ErrSessionNotFound
	}
	in, err := r.store.PlanInput(ctx)
	if err != nil {
		return changeSet{}, fmt.Errorf("load plan input: %w", err)
	}
	in.Now = now
	snap := plan.RulesFor(plan.Daemon{Workflows: config.Workflows()}, in.Obs).Derive(in)
	entry, ok := snap.Entry(plan.TicketURL(row.URL))
	if !ok {
		return changeSet{}, ErrSessionNotFound
	}
	tip := in.Obs.LocalTips[entry.Ticket.Ref()]
	if tip == "" {
		return changeSet{}, ErrNoLocalTip
	}
	base := entry.Unlock.BaseBranch
	diffs, err := git.Diff(ctx, config.CheckoutPath(dataDir, row.Repo), "origin/"+base, string(tip))
	if err != nil {
		return changeSet{}, fmt.Errorf("diff %s against origin/%s: %w", entry.Ticket.Branch, base, err)
	}
	return changeSet{board: board, row: row, in: in, entry: entry, tip: tip, base: base, diffs: diffs}, nil
}

func (r *Reader) Changes(ctx context.Context, now time.Time, ref SessionRef, dataDir string) (ChangesPage, error) {
	cs, err := r.loadChanges(ctx, now, ref, dataDir)
	if err != nil {
		return ChangesPage{}, err
	}
	board, row, in, entry, tip, base, diffs := cs.board, cs.row, cs.in, cs.entry, cs.tip, cs.base, cs.diffs
	pending, err := r.store.ReviewComments(ctx, plan.TicketURL(row.URL))
	if err != nil {
		return ChangesPage{}, fmt.Errorf("load review comments: %w", err)
	}

	paths := make([]string, len(diffs))
	for i, d := range diffs {
		paths[i] = d.Path
	}
	settings := in.Obs.Settings[row.Repo]
	matches := plan.Classify(paths, plan.Policy{Deny: settings.Deny, Protect: settings.Protect})
	changes := Changes{
		Ticket: plan.TicketURL(row.URL), Base: base, Tip: tip, State: entry.State, PR: option.None[string](),
		Comments:    map[string][]plan.ReviewComment{},
		CanFollowUp: slices.Contains(plan.Verbs(entry.State), plan.VerbFollowUp),
	}
	for _, c := range pending {
		changes.Comments[c.Path] = append(changes.Comments[c.Path], c)
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
		Reason:      entry.Reason,
	}, nil
}

func lineSide(l git.DiffLine) (plan.Side, int) {
	if l.Kind == git.LineDeleted {
		return plan.SideOld, l.OldLine
	}
	return plan.SideNew, l.NewLine
}

func (p ChangesPage) At(path string, l git.DiffLine) []plan.ReviewComment {
	side, line := lineSide(l)
	var out []plan.ReviewComment
	for _, c := range p.Comments[path] {
		if c.Side == side && c.Line == line {
			out = append(out, c)
		}
	}
	return out
}

type ReviewThread struct {
	SessionPath string
	Comment     plan.ReviewComment
	Pending     int
}

type ReviewForm struct {
	SessionPath string
	Path        string
	Side        plan.Side
	Line        int
}

func (p ChangesPage) Thread(c plan.ReviewComment) ReviewThread {
	return ReviewThread{SessionPath: p.SessionPath, Comment: c}
}

var ErrNoSuchLine = errors.New("view: no such diff line")

type CommentAnchor struct {
	Ticket plan.TicketURL
	Tip    plan.SHA
	Hunk   string
}

func (r *Reader) CommentAnchor(
	ctx context.Context, now time.Time, ref SessionRef, dataDir, path string, side plan.Side, line int,
) (CommentAnchor, error) {
	cs, err := r.loadChanges(ctx, now, ref, dataDir)
	if err != nil {
		return CommentAnchor{}, err
	}
	anchor := CommentAnchor{Ticket: plan.TicketURL(cs.row.URL), Tip: cs.tip}
	for _, f := range cs.diffs {
		if f.Path != path {
			continue
		}
		for _, h := range f.Hunks {
			var b strings.Builder
			b.WriteString(h.Header)
			for _, l := range h.Lines {
				b.WriteString("\n" + linePrefix(l.Kind) + l.Text)
				if s, n := lineSide(l); s == side && n == line {
					anchor.Hunk = b.String()
					return anchor, nil
				}
			}
		}
	}
	return CommentAnchor{}, ErrNoSuchLine
}

func linePrefix(k git.LineKind) string {
	switch k {
	case git.LineAdded:
		return "+"
	case git.LineDeleted:
		return "-"
	case git.LineContext:
		return " "
	}
	return " "
}

func (r *Reader) SessionTicket(ctx context.Context, now time.Time, ref SessionRef) (plan.TicketURL, error) {
	board, err := r.Board(ctx, now, Params{})
	if err != nil {
		return "", fmt.Errorf("load board: %w", err)
	}
	row, ok := board.rowFor(ref)
	if !ok {
		return "", ErrSessionNotFound
	}
	return plan.TicketURL(row.URL), nil
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
