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
	ID        string // "f-<index>"
	Protected option.Option[plan.PathMatch]
	Gaps      []int // hidden lines before each hunk; len(Gaps) == len(Hunks)
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

type TreeDir struct {
	Name  string // compacted: "internal/loop"
	Path  string
	Dirs  []TreeDir
	Files []int // indexes into Changes.Files
}

type ChangesPage struct {
	Chrome
	Changes
	SessionPath string
	ViewedKey   string
	Tree        TreeDir
	Reason      plan.Reason
	Request     RequestChanges
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
	for i, d := range diffs {
		file := ChangedFile{
			FileDiff: d, ID: "f-" + strconv.Itoa(i), Protected: option.None[plan.PathMatch](), Gaps: hunkGaps(d.Hunks),
		}
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
		Tree:        buildTree(changes.Files),
		Reason:      entry.Reason,
		Request:     NewRequestChanges(self, tip, changes.CanFollowUp, entry.Reason, "", pending),
	}, nil
}

func lineSide(l git.DiffLine) (plan.Side, int) {
	if l.Kind == git.LineDeleted {
		return plan.SideOld, l.OldLine
	}
	return plan.SideNew, l.NewLine
}

func (p ChangesPage) At(path string, l git.DiffLine) []plan.ReviewComment {
	return commentsAt(p.Comments[path], l)
}

func commentsAt(comments []plan.ReviewComment, l git.DiffLine) []plan.ReviewComment {
	side, line := lineSide(l)
	var out []plan.ReviewComment
	for _, c := range comments {
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
	Restored    bool
}

type ReviewForm struct {
	SessionPath string
	Path        string
	Side        plan.Side
	Line        int
}

func (t ReviewThread) LineID() string { return lineID(t.Comment.Path, t.Comment.Side, t.Comment.Line) }

func (t ReviewThread) Vals() (string, error) { return restoreVals(t.Comment) }

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
	inDiff := slices.ContainsFunc(cs.diffs, func(f git.FileDiff) bool { return f.Path == target.Path })
	if !inDiff || target.Side != plan.SideNew {
		return CommentAnchor{}, ErrNoSuchLine
	}
	text, err := git.FileLines(
		ctx, config.CheckoutPath(r.dataDir, cs.row.Repo), string(cs.tip), target.Path, target.Line, target.Line)
	if err != nil {
		return CommentAnchor{}, fmt.Errorf("read %s line %d: %w", target.Path, target.Line, err)
	}
	if len(text) == 0 {
		return CommentAnchor{}, ErrNoSuchLine
	}
	anchor.Hunk = fmt.Sprintf("@@ +%d @@\n %s", target.Line, text[0])
	return anchor, nil
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

func buildTree(files []ChangedFile) TreeDir {
	var root TreeDir
	for i, f := range files {
		dir := &root
		if d := path.Dir(f.Path); d != "." {
			for _, seg := range strings.Split(d, "/") {
				j := slices.IndexFunc(dir.Dirs, func(c TreeDir) bool { return c.Name == seg })
				if j < 0 {
					dir.Dirs = append(dir.Dirs, TreeDir{Name: seg, Path: path.Join(dir.Path, seg)})
					j = len(dir.Dirs) - 1
				}
				dir = &dir.Dirs[j]
			}
		}
		dir.Files = append(dir.Files, i)
	}
	root.compact()
	return root
}

func (d *TreeDir) compact() {
	for i := range d.Dirs {
		c := &d.Dirs[i]
		for len(c.Files) == 0 && len(c.Dirs) == 1 {
			only := c.Dirs[0]
			c.Name += "/" + only.Name
			c.Path, c.Dirs, c.Files = only.Path, only.Dirs, only.Files
		}
		c.compact()
	}
	slices.SortFunc(d.Dirs, func(a, b TreeDir) int { return cmp.Compare(a.Name, b.Name) })
}

type TreeNode struct {
	TreeDir
	files []ChangedFile
}

func (p ChangesPage) TreeRoot() TreeNode { return TreeNode{TreeDir: p.Tree, files: p.Files} }

func (n TreeNode) Subdirs() []TreeNode {
	nodes := make([]TreeNode, len(n.Dirs))
	for i, d := range n.Dirs {
		nodes[i] = TreeNode{TreeDir: d, files: n.files}
	}
	return nodes
}

func (n TreeNode) Members() []ChangedFile {
	members := make([]ChangedFile, len(n.Files))
	for i, idx := range n.Files {
		members[i] = n.files[idx]
	}
	return members
}

func (d TreeDir) Total() int {
	n := len(d.Files)
	for _, c := range d.Dirs {
		n += c.Total()
	}
	return n
}

func (f ChangedFile) Dir() string {
	if d := path.Dir(f.Path); d != "." {
		return d + "/"
	}
	return ""
}

func (f ChangedFile) Base() string { return path.Base(f.Path) }

func (f ChangedFile) KindLabel() string {
	k := string(f.Kind)
	if k == "" {
		return ""
	}
	return strings.ToUpper(k[:1]) + k[1:]
}

type gapSpan struct {
	firstNew    int
	count       int
	oldMinusNew int
}

func gapSpans(hunks []git.Hunk) []gapSpan {
	spans := make([]gapSpan, len(hunks))
	next := 1
	for i, h := range hunks {
		first, oldFirst := h.NewStart, h.OldStart
		if h.NewLines == 0 {
			first++
		}
		if h.OldLines == 0 {
			oldFirst++
		}
		spans[i] = gapSpan{firstNew: next, count: max(0, first-next), oldMinusNew: oldFirst - first}
		next = first + h.NewLines
	}
	return spans
}

func hunkGaps(hunks []git.Hunk) []int {
	gaps := make([]int, len(hunks))
	for i, s := range gapSpans(hunks) {
		gaps[i] = s.count
	}
	return gaps
}

func (f ChangedFile) Bar() [5]string {
	var cells [5]string
	total := f.Added + f.Deleted
	if total == 0 {
		return cells
	}
	added := (f.Added*5 + total/2) / total
	for i := range cells {
		cells[i] = "deleted"
		if i < added {
			cells[i] = "added"
		}
	}
	return cells
}
