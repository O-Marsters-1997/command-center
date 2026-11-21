package view

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/url"
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
	CanRaise    bool
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

func (r *Reader) loadChanges(ctx context.Context, now time.Time, ref SessionRef) (changeSet, error) {
	board, row, err := r.sessionRow(ctx, now, ref)
	if err != nil {
		return changeSet{}, err
	}
	in := board.in
	entry, ok := board.snap.Entry(plan.TicketURL(row.URL))
	if !ok {
		return changeSet{}, ErrSessionNotFound
	}
	tip := in.Obs.LocalTips[entry.Ticket.Ref()]
	if tip == "" {
		return changeSet{}, ErrNoLocalTip
	}
	base := entry.Unlock.BaseBranch
	diffs, err := git.Diff(ctx, config.CheckoutPath(r.dataDir, row.Repo), "origin/"+base, string(tip))
	if err != nil {
		return changeSet{}, fmt.Errorf("diff %s against origin/%s: %w", entry.Ticket.Branch, base, err)
	}
	return changeSet{board: board, row: row, in: in, entry: entry, tip: tip, base: base, diffs: diffs}, nil
}

func (r *Reader) Changes(ctx context.Context, now time.Time, ref SessionRef) (ChangesPage, error) {
	cs, err := r.loadChanges(ctx, now, ref)
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
		CanRaise:    slices.Contains(plan.Verbs(entry.State), plan.VerbRaise),
	}
	for _, c := range pending {
		changes.Comments[c.Path] = append(changes.Comments[c.Path], c)
	}
	if row.PRURL != "" {
		changes.PR = option.Some(row.PRURL)
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

type DiffLine struct {
	Path string
	Side plan.Side
	Line int
}

func (r *Reader) CommentAnchor(
	ctx context.Context, now time.Time, ref SessionRef, target DiffLine,
) (CommentAnchor, error) {
	cs, err := r.loadChanges(ctx, now, ref)
	if err != nil {
		return CommentAnchor{}, err
	}
	anchor := CommentAnchor{Ticket: plan.TicketURL(cs.row.URL), Tip: cs.tip}
	for _, f := range cs.diffs {
		if f.Path != target.Path {
			continue
		}
		for _, h := range f.Hunks {
			var b strings.Builder
			b.WriteString(h.Header)
			for _, l := range h.Lines {
				b.WriteString("\n" + linePrefix(l.Kind) + l.Text)
				if s, n := lineSide(l); s == target.Side && n == target.Line {
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
	_, row, err := r.sessionRow(ctx, now, ref)
	if err != nil {
		return "", err
	}
	return plan.TicketURL(row.URL), nil
}

func (r *Reader) SessionSnapshot(
	ctx context.Context, now time.Time, ref SessionRef,
) (plan.TicketURL, plan.Snapshot, error) {
	board, row, err := r.sessionRow(ctx, now, ref)
	if err != nil {
		return "", plan.Snapshot{}, err
	}
	return plan.TicketURL(row.URL), board.snap, nil
}

type RaiseResult struct {
	PRURL   string
	Problem plan.Reason
}

func (r *Reader) RaiseResult(ctx context.Context, now time.Time, ref SessionRef) (RaiseResult, error) {
	board, row, err := r.sessionRow(ctx, now, ref)
	if err != nil {
		return RaiseResult{}, err
	}
	entry, ok := board.snap.Entry(plan.TicketURL(row.URL))
	if !ok {
		return RaiseResult{}, ErrSessionNotFound
	}
	if entry.State == plan.AwaitingPR {
		return RaiseResult{Problem: "the tip has moved since you loaded this page; reload to see the new diff"}, nil
	}
	if entry.State == plan.PushFailed || entry.State == plan.PushRefused {
		return RaiseResult{Problem: entry.Reason}, nil
	}
	if row.PRURL != "" {
		return RaiseResult{PRURL: row.PRURL}, nil
	}
	search := "https://github.com/" + row.Repo + "/pulls?q=head%3A" + url.QueryEscape(entry.Ticket.Branch)
	return RaiseResult{PRURL: search}, nil
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
