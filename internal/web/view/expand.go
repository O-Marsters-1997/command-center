package view

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/adapter/git"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

const expandStep = 20

type ExpandEdge string

const (
	ExpandAbove ExpandEdge = "above"
	ExpandBelow ExpandEdge = "below"
	ExpandAll   ExpandEdge = "all"
)

type Expand struct {
	Path string
	Hunk int // the hunk whose gap is expanded
	Edge ExpandEdge
}

type LineRow struct {
	git.DiffLine
	SessionPath string
	Path        string
	Comments    []plan.ReviewComment
}

func (r LineRow) Thread(c plan.ReviewComment) ReviewThread {
	return ReviewThread{SessionPath: r.SessionPath, Comment: c}
}

func (r LineRow) Side() plan.Side {
	side, _ := lineSide(r.DiffLine)
	return side
}

func (r LineRow) Number() int {
	_, n := lineSide(r.DiffLine)
	return n
}

func (r LineRow) CommentURL() string {
	q := url.Values{"path": {r.Path}, "side": {string(r.Side())}, "line": {strconv.Itoa(r.Number())}}
	return r.SessionPath + "/changes/comment?" + q.Encode()
}

type GapRow struct {
	SessionPath string
	Path        string
	Hunk        int
	Above       int // lines already revealed from the top edge
	Below       int // lines already revealed from the bottom edge
	Remaining   int
	Section     string
}

func (g GapRow) CanExpandAll() bool { return g.Remaining < expandStep }

func (g GapRow) ExpandURL(edge ExpandEdge) string {
	q := url.Values{
		"path": {g.Path}, "hunk": {strconv.Itoa(g.Hunk)}, "edge": {string(edge)},
		"above": {strconv.Itoa(g.Above)}, "below": {strconv.Itoa(g.Below)},
	}
	return g.SessionPath + "/changes/expand?" + q.Encode()
}

type Expansion struct {
	Edge ExpandEdge
	Rows []LineRow
	Gap  GapRow
}

func (p ChangesPage) Row(path string, l git.DiffLine) LineRow {
	return LineRow{DiffLine: l, SessionPath: p.SessionPath, Path: path, Comments: p.At(path, l)}
}

func (p ChangesPage) Gap(f ChangedFile, hunk int) GapRow {
	return GapRow{
		SessionPath: p.SessionPath, Path: f.Path, Hunk: hunk, Remaining: f.Gaps[hunk], Section: f.Hunks[hunk].Section,
	}
}

func (r *Reader) Expanded(ctx context.Context, now time.Time, ref SessionRef, e Expand) ([]git.DiffLine, error) {
	x, err := r.ExpandMore(ctx, now, ref, e, 0, 0)
	if err != nil {
		return nil, err
	}
	lines := make([]git.DiffLine, len(x.Rows))
	for i, row := range x.Rows {
		lines[i] = row.DiffLine
	}
	return lines, nil
}

func (r *Reader) ExpandMore(
	ctx context.Context, now time.Time, ref SessionRef, e Expand, above, below int,
) (Expansion, error) {
	cs, err := r.loadChanges(ctx, now, ref)
	if err != nil {
		return Expansion{}, err
	}
	i := slices.IndexFunc(cs.diffs, func(d git.FileDiff) bool { return d.Path == e.Path })
	if i < 0 || e.Hunk < 0 || e.Hunk >= len(cs.diffs[i].Hunks) {
		return Expansion{}, ErrNoSuchLine
	}
	hunks := cs.diffs[i].Hunks
	span := gapSpans(hunks)[e.Hunk]
	left := span.count - above - below
	if above < 0 || below < 0 || left < 0 {
		return Expansion{}, ErrNoSuchLine
	}
	n := min(expandStep, left)
	first := span.first + above
	switch e.Edge {
	case ExpandAbove:
	case ExpandBelow:
		first = span.first + span.count - below - n
	case ExpandAll:
		n = left
	default:
		return Expansion{}, ErrNoSuchLine
	}
	var texts []string
	if n > 0 {
		texts, err = git.FileLines(
			ctx, config.CheckoutPath(r.dataDir, cs.row.Repo), string(cs.tip), e.Path, first, first+n-1)
		if err != nil {
			return Expansion{}, fmt.Errorf("read %s lines %d..%d: %w", e.Path, first, first+n-1, err)
		}
	}
	pending, err := r.store.ReviewComments(ctx, plan.TicketURL(cs.row.URL))
	if err != nil {
		return Expansion{}, fmt.Errorf("load review comments: %w", err)
	}
	var onPath []plan.ReviewComment
	for _, c := range pending {
		if c.Path == e.Path {
			onPath = append(onPath, c)
		}
	}
	self := ref.Path()
	rows := make([]LineRow, len(texts))
	for j, text := range texts {
		l := git.DiffLine{Kind: git.LineContext, OldLine: first + j + span.delta, NewLine: first + j, Text: text}
		rows[j] = LineRow{DiffLine: l, SessionPath: self, Path: e.Path, Comments: commentsAt(onPath, l)}
	}
	if e.Edge == ExpandBelow {
		below += len(rows)
	} else {
		above += len(rows)
	}
	return Expansion{
		Edge: e.Edge,
		Rows: rows,
		Gap: GapRow{
			SessionPath: self, Path: e.Path, Hunk: e.Hunk, Above: above, Below: below,
			Remaining: span.count - above - below, Section: hunks[e.Hunk].Section,
		},
	}, nil
}
