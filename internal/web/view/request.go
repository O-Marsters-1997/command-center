package view

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

type RequestChanges struct {
	SessionPath string
	Tip         plan.SHA
	Note        string
	Prompt      plan.FollowUpPrompt
	Comments    []plan.ReviewComment // sorted by path, then line
	Models      []string
	Model       string // config.AgentModel unless chosen
	Efforts     []plan.Effort
	Effort      plan.Effort // "" is the default effort
	CanSend     bool
	Why         string // shown beside a disabled send
}

const hunkTailLines = 4

func NewRequestChanges(
	sessionPath string, tip plan.SHA, offered bool, reason plan.Reason, note string, comments []plan.ReviewComment,
) RequestChanges {
	note = strings.TrimSpace(note)
	sorted := slices.Clone(comments)
	slices.SortStableFunc(sorted, func(a, b plan.ReviewComment) int {
		return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.Line, b.Line))
	})
	r := RequestChanges{
		SessionPath: sessionPath,
		Tip:         tip,
		Note:        note,
		Prompt: plan.FollowUpPrompt{
			Message:    note,
			Comments:   len(sorted),
			Attachment: plan.RunIDPlaceholder + "." + plan.ReviewAttachmentName,
		},
		Comments: sorted,
		Models:   config.AgentModels(),
		Model:    config.AgentModel,
		Efforts:  plan.AllEfforts(),
	}
	switch {
	case !offered:
		r.Why = string(reason)
	case note == "" && len(sorted) == 0:
		r.Why = "Write what to change, or comment on the diff first."
	default:
		r.CanSend = true
	}
	return r
}

func (r RequestChanges) Subtitle() string {
	return "Branch tip " + sha8(r.Tip) + " · " + r.Pending()
}

func (r RequestChanges) Pending() string {
	return countNoun(len(r.Comments), "pending comment", "pending comments")
}

func (r RequestChanges) Attached() string {
	return countNoun(len(r.Comments), "comment", "comments")
}

func sha8(s plan.SHA) string { return string(s[:min(len(s), 8)]) }

func countNoun(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

type PreviewParts struct {
	Before, Note, After string
}

func (r RequestChanges) Preview() PreviewParts {
	if r.Note == "" {
		return PreviewParts{Before: r.Prompt.Compose()}
	}
	const slot = "\x00note\x00"
	marked := r.Prompt
	marked.Message = slot
	before, after, found := strings.Cut(marked.Compose(), slot)
	if !found {
		return PreviewParts{Before: r.Prompt.Compose()}
	}
	return PreviewParts{Before: before, Note: r.Note, After: after}
}

func (r RequestChanges) Sent(model string, effort plan.Effort) SentRequest {
	return SentRequest{
		SessionPath: r.SessionPath, Comments: r.Comments, Count: r.Attached(),
		Model: model, Effort: effort,
	}
}

type SentRequest struct {
	SessionPath string
	Comments    []plan.ReviewComment
	Count       string
	Model       string
	Effort      plan.Effort
}

func (s SentRequest) Meta() string {
	effort := string(s.Effort)
	if effort == "" {
		effort = "default effort"
	}
	return s.Count + " · " + s.Model + " · " + effort
}

func (s SentRequest) Shown() []plan.ReviewComment {
	return s.Comments[:min(len(s.Comments), 8)]
}

func (s SentRequest) More() int { return max(0, len(s.Comments)-8) }

func (r RequestChanges) NoteHint() string {
	if len(r.Comments) > 0 {
		return "optional when there are comments"
	}
	return "required with no comments"
}

func (r RequestChanges) JSON() (string, error) {
	if len(r.Comments) == 0 {
		return "[]", nil
	}
	raw, err := json.MarshalIndent(r.Comments, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode review comments: %w", err)
	}
	return string(raw), nil
}

type RequestPart struct {
	RequestChanges
	Swap bool
}

func (r RequestChanges) Part() RequestPart { return RequestPart{RequestChanges: r} }

func (r RequestChanges) SwapPart() RequestPart { return RequestPart{RequestChanges: r, Swap: true} }

type HunkLine struct {
	Text string
	Kind string // "added", "deleted" or ""
}

type RequestItem struct {
	SessionPath string
	Comment     plan.ReviewComment
}

func (r RequestChanges) Items() []RequestItem {
	items := make([]RequestItem, len(r.Comments))
	for i, c := range r.Comments {
		items[i] = RequestItem{SessionPath: r.SessionPath, Comment: c}
	}
	return items
}

func (i RequestItem) Where() string { return i.Comment.Path + ":" + strconv.Itoa(i.Comment.Line) }

func (i RequestItem) Side() string {
	if i.Comment.Side == plan.SideOld {
		return "Removed line " + strconv.Itoa(i.Comment.Line)
	}
	return "Line " + strconv.Itoa(i.Comment.Line)
}

func (i RequestItem) Tail() []HunkLine {
	lines := strings.Split(i.Comment.Hunk, "\n")
	lines = lines[max(0, len(lines)-hunkTailLines):]
	out := make([]HunkLine, len(lines))
	for n, l := range lines {
		out[n].Text = l
		switch {
		case strings.HasPrefix(l, "+"):
			out[n].Kind = "added"
		case strings.HasPrefix(l, "-"):
			out[n].Kind = "deleted"
		}
	}
	return out
}

type RemovedComment struct {
	RequestItem
	Part RequestPart
}

func (c RemovedComment) Vals() (string, error) {
	raw, err := json.Marshal(map[string]any{
		"tip": c.Comment.Tip, "path": c.Comment.Path, "line": c.Comment.Line,
		"side": c.Comment.Side, "hunk": c.Comment.Hunk, "body": c.Comment.Body,
	})
	if err != nil {
		return "", fmt.Errorf("encode removed comment: %w", err)
	}
	return string(raw), nil
}

type RestoredComment struct {
	RequestItem
	Part RequestPart
}

func (r *Reader) RequestChanges(
	ctx context.Context, now time.Time, ref SessionRef, note string,
) (RequestChanges, error) {
	board, row, err := r.sessionRow(ctx, now, ref)
	if err != nil {
		return RequestChanges{}, err
	}
	entry, ok := board.snap.Entry(plan.TicketURL(row.URL))
	if !ok {
		return RequestChanges{}, ErrSessionNotFound
	}
	comments, err := r.store.ReviewComments(ctx, plan.TicketURL(row.URL))
	if err != nil {
		return RequestChanges{}, fmt.Errorf("load review comments: %w", err)
	}
	tip := board.in.Obs.LocalTips[entry.Ticket.Ref()]
	offered := slices.Contains(plan.Verbs(entry.State), plan.VerbFollowUp)
	return NewRequestChanges(ref.Path(), tip, offered, entry.Reason, note, comments), nil
}
