package view

import (
	"context"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

const (
	trayRecent  = 8
	trayHistory = 24 * time.Hour
)

type Tray struct {
	Glyph      plan.GlyphWord
	InProgress []TrayItem
	Recent     []TrayLine
}

type TrayItem struct {
	ID        int64
	Ticket    string
	Verb      plan.Verb
	Action    string
	Ref       string
	Phase     string
	Glyph     plan.GlyphWord
	Since     Age
	UndoUntil option.Option[time.Time]
	Auto      bool
	Path      string
}

type TrayLine struct {
	At     time.Time
	Action string
	Ref    string
	Phase  string
}

func buildTray(records []plan.IntentRecord, runs map[plan.TicketURL]plan.RunSummary, now time.Time) Tray {
	var tray Tray
	var lines []TrayLine
	for _, rec := range records {
		run := option.None[plan.RunSummary]()
		if s, ok := runs[rec.Ticket]; ok && rec.Run.IsSome() && s.ID == rec.Run.Unwrap() {
			run = option.Some(s)
		}
		st := plan.StatusOf(rec, run, now)
		ticket := string(rec.Ticket)
		ref := trayRef(ticket)
		for _, step := range plan.Trail(st) {
			lines = append(lines, TrayLine{At: step.At, Action: string(rec.Verb), Ref: ref, Phase: stepPhase(step)})
		}
		if st.Phase != plan.PhaseQueued && st.Phase != plan.PhaseHeld && st.Phase != plan.PhaseRunning {
			continue
		}
		item := TrayItem{
			ID: rec.ID, Ticket: ticket, Verb: rec.Verb, Action: string(rec.Verb), Ref: ref,
			Phase: phaseLabel(st.Phase), Glyph: st.Glyph, Auto: rec.QueuedBy == plan.QueuedByLoop,
			Since: Age{Age: shortAge(now.Sub(rec.At)), Stamp: rec.At.UTC().Format(time.RFC3339)},
			Path:  SessionPath(ticket),
		}
		if st.Phase == plan.PhaseHeld {
			item.UndoUntil = option.Some(rec.HoldUntil)
		}
		tray.InProgress = append(tray.InProgress, item)
		if tray.Glyph != plan.GlyphRunning {
			tray.Glyph = st.Glyph
		}
	}
	slices.SortStableFunc(lines, func(a, b TrayLine) int { return b.At.Compare(a.At) })
	tray.Recent = lines[:min(len(lines), trayRecent)]
	return tray
}

func trayRef(ticketURL string) string {
	ref, err := ParseSessionRef(plan.TicketURL(ticketURL))
	if err != nil {
		return path.Base(ticketURL)
	}
	return ref.Name + "#" + ref.N
}

func phaseLabel(p plan.IntentPhase) string { return strings.ReplaceAll(string(p), "_", " ") }

func stepPhase(s plan.IntentStep) string {
	if s.Outcome != plan.IntentInProgress {
		return string(s.Outcome)
	}
	return phaseLabel(s.Phase)
}

func (r *Reader) Tray(ctx context.Context, now time.Time) (Tray, error) {
	history, err := r.store.IntentHistory(ctx, now.Add(-trayHistory))
	if err != nil {
		return Tray{}, fmt.Errorf("load intent history: %w", err)
	}
	runs, err := r.store.LatestRunsByTicket(ctx)
	if err != nil {
		return Tray{}, fmt.Errorf("load latest runs: %w", err)
	}
	return buildTray(history, runs, now), nil
}

func applyActivities(rows []Row, activities []store.Activity, now time.Time) {
	byTicket := make(map[string]store.Activity, len(activities))
	for _, a := range activities {
		byTicket[a.TicketURL] = a
	}
	for i := range rows {
		a, ok := byTicket[rows[i].URL]
		if !ok {
			continue
		}
		rows[i].Updated = Age{Age: shortAge(now.Sub(a.At)), Stamp: a.At.UTC().Format(time.RFC3339)}
		rows[i].Last = firstLine(a.LastMessage)
	}
}

func shortAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func firstLine(s string) string {
	for line := range strings.SplitSeq(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}
