package view

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

const (
	activityPageSize  = 25
	activityRetention = 30 * 24 * time.Hour
)

type ActivityRange string

const (
	RangeAny   ActivityRange = ""
	RangeToday ActivityRange = "today"
	Range7d    ActivityRange = "7d"
	Range30d   ActivityRange = "30d"
)

func (r ActivityRange) since(now time.Time) time.Time {
	switch r {
	case RangeToday:
		y, m, d := now.Date()
		return time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	case Range7d:
		return now.AddDate(0, 0, -7)
	case Range30d:
		return now.Add(-activityRetention)
	case RangeAny:
	}
	return time.Time{}
}

func (r ActivityRange) phrase() string {
	switch r {
	case RangeToday:
		return " today"
	case Range7d:
		return " in the last 7 days"
	case Range30d:
		return " in the last 30 days"
	case RangeAny:
	}
	return ""
}

type ActivityParams struct {
	Query   string
	Outcome plan.IntentOutcome
	Verb    plan.Verb
	Project string
	Feature string
	Range   ActivityRange
	Page    int
}

func ParseActivityParams(q url.Values, rail Params) ActivityParams {
	p := ActivityParams{
		Query: strings.TrimSpace(q.Get("q")), Project: rail.Project, Feature: rail.Feature, Page: 1,
	}
	switch o := plan.IntentOutcome(q.Get("outcome")); o {
	case plan.IntentDone, plan.IntentFailed, plan.IntentWithdrawn:
		p.Outcome = o
	case plan.IntentInProgress:
	}
	if verb, err := plan.ParseVerb(q.Get("verb")); err == nil {
		p.Verb = verb
	}
	if q.Has("project") {
		p.Project = q.Get("project")
	}
	if q.Has("feature") {
		p.Feature = q.Get("feature")
	}
	switch r := ActivityRange(q.Get("range")); r {
	case RangeToday, Range7d, Range30d:
		p.Range = r
	case RangeAny:
	}
	if n, err := strconv.Atoi(q.Get("page")); err == nil && n > 1 {
		p.Page = n
	}
	return p
}

func (p ActivityParams) Filtered() bool {
	return p.Query != "" || p.Outcome != plan.IntentInProgress || p.Verb != plan.VerbUnknown ||
		p.Project != "" || p.Feature != "" || p.Range != RangeAny
}

func (p ActivityParams) Encode() string {
	q := url.Values{}
	for k, v := range map[string]string{
		"q": p.Query, "outcome": string(p.Outcome), "verb": string(p.Verb),
		"project": p.Project, "feature": p.Feature, "range": string(p.Range),
	} {
		if v != "" {
			q.Set(k, v)
		}
	}
	if p.Page > 1 {
		q.Set("page", strconv.Itoa(p.Page))
	}
	return q.Encode()
}

func (p ActivityParams) pagePath(page int) string {
	p.Page = page
	return withQuery("/activity", p.Encode())
}

type ActivityPage struct {
	Chrome
	Params   ActivityParams
	Counts   map[plan.IntentOutcome]int
	Days     []ActivityDay
	Total    int
	Page     int
	Pages    int
	Verbs    []plan.Verb
	Empty    string
	Memory   MemoryChart
	features []string
	readErr  string
}

func (p ActivityPage) ReadError() string { return p.readErr }

func (p ActivityPage) All() int {
	return p.Counts[plan.IntentDone] + p.Counts[plan.IntentFailed] + p.Counts[plan.IntentWithdrawn]
}

func (p ActivityPage) Features() []string { return p.features }

func (p ActivityPage) First() int { return min((p.Page-1)*activityPageSize+1, p.Total) }
func (p ActivityPage) Last() int  { return min(p.Page*activityPageSize, p.Total) }

func (p ActivityPage) NewerPath() string {
	if p.Page <= 1 {
		return ""
	}
	return p.Params.pagePath(p.Page - 1)
}

func (p ActivityPage) OlderPath() string {
	if p.Page >= p.Pages {
		return ""
	}
	return p.Params.pagePath(p.Page + 1)
}

func (p ActivityPage) ClearPath() string {
	return withQuery("/activity", Params{Project: p.ProjectScope}.Query())
}

type ActivityTab struct {
	Label   string
	Value   plan.IntentOutcome
	Glyph   plan.GlyphWord
	Count   int
	Current bool
}

var activityOutcomes = []ActivityTab{
	{Label: "Done", Value: plan.IntentDone, Glyph: plan.GlyphDone},
	{Label: "Failed", Value: plan.IntentFailed, Glyph: plan.GlyphFailed},
	{Label: "Withdrawn", Value: plan.IntentWithdrawn, Glyph: plan.GlyphPending},
}

func tallies(counts map[plan.IntentOutcome]int) []ActivityTab {
	tabs := slices.Clone(activityOutcomes)
	for i := range tabs {
		tabs[i].Count = counts[tabs[i].Value]
	}
	return tabs
}

func (p ActivityPage) Tabs() []ActivityTab {
	all := ActivityTab{Label: "All", Count: p.All(), Glyph: plan.GlyphPending}
	tabs := append([]ActivityTab{all}, tallies(p.Counts)...)
	for i := range tabs {
		tabs[i].Current = tabs[i].Value == p.Params.Outcome
	}
	return tabs
}

type ActivityDay struct {
	Key    string
	Label  string
	Counts map[plan.IntentOutcome]int
	Rows   []ActivityRow
}

func (d ActivityDay) Tallies() []ActivityTab { return tallies(d.Counts) }

type ActivityRow struct {
	ID      int64
	Queued  time.Time
	Glyph   plan.GlyphWord
	Action  string
	Auto    bool
	Ref     string
	Title   string
	Feature string
	Outcome plan.IntentOutcome
	Detail  string
	Took    time.Duration
	Trail   []plan.IntentStep
	Session string
}

func (r ActivityRow) Offset(s plan.IntentStep) string {
	return "+" + s.At.Sub(r.Queued).Round(time.Second).String()
}

func (r ActivityRow) Duration() string { return r.Took.Round(time.Second).String() }

func (r ActivityRow) matches(p ActivityParams, now time.Time) bool {
	if r.Queued.Before(p.Range.since(now)) || (p.Verb != plan.VerbUnknown && r.Action != string(p.Verb)) {
		return false
	}
	if p.Query == "" {
		return true
	}
	needle := strings.ToLower(p.Query)
	for _, field := range []string{r.Ref, r.Title, r.Feature, r.Detail} {
		if strings.Contains(strings.ToLower(field), needle) {
			return true
		}
	}
	return false
}

func NewActivityPage(
	chrome Chrome, p ActivityParams, rows []ActivityRow, features []string, now time.Time,
) ActivityPage {
	page := ActivityPage{Chrome: chrome, Params: p, Counts: map[plan.IntentOutcome]int{}, features: features}
	var matched []ActivityRow
	for _, r := range rows {
		if !slices.Contains(page.Verbs, plan.Verb(r.Action)) {
			page.Verbs = append(page.Verbs, plan.Verb(r.Action))
		}
		if !r.matches(p, now) {
			continue
		}
		page.Counts[r.Outcome]++
		if p.Outcome == plan.IntentInProgress || r.Outcome == p.Outcome {
			matched = append(matched, r)
		}
	}
	slices.Sort(page.Verbs)
	page.Total = len(matched)
	page.Pages = max(1, (page.Total+activityPageSize-1)/activityPageSize)
	page.Page = min(p.Page, page.Pages)
	page.Params.Page = page.Page
	lo := (page.Page - 1) * activityPageSize
	page.Days = groupDays(matched[lo:min(lo+activityPageSize, len(matched))], now)
	page.Empty = emptyActivity(p, len(rows))
	return page
}

func groupDays(rows []ActivityRow, now time.Time) []ActivityDay {
	var days []ActivityDay
	for _, r := range rows {
		local := r.Queued.In(now.Location())
		key := local.Format(time.DateOnly)
		if len(days) == 0 || days[len(days)-1].Key != key {
			days = append(days, ActivityDay{Key: key, Label: dayLabel(local, now), Counts: map[plan.IntentOutcome]int{}})
		}
		day := &days[len(days)-1]
		day.Rows = append(day.Rows, r)
		day.Counts[r.Outcome]++
	}
	return days
}

func dayLabel(day, now time.Time) string {
	switch day.Format(time.DateOnly) {
	case now.Format(time.DateOnly):
		return "Today"
	case now.AddDate(0, 0, -1).Format(time.DateOnly):
		return "Yesterday"
	}
	return day.Format("Mon 2 Jan")
}

func emptyActivity(p ActivityParams, history int) string {
	if history == 0 && !p.Filtered() {
		return "No actions in the last 30 days."
	}
	var b strings.Builder
	b.WriteString("No ")
	if p.Outcome != plan.IntentInProgress {
		b.WriteString(string(p.Outcome) + " ")
	}
	b.WriteString("actions")
	if p.Verb != plan.VerbUnknown {
		b.WriteString(" of " + string(p.Verb))
	}
	if p.Project != "" {
		b.WriteString(" in " + p.Project)
	}
	if p.Feature != "" {
		b.WriteString(" on " + p.Feature)
	}
	if p.Query != "" {
		fmt.Fprintf(&b, " match '%s'", p.Query)
	}
	b.WriteString(p.Range.phrase() + ".")
	return b.String()
}

func (r *Reader) ActivityPage(ctx context.Context, now time.Time, q url.Values) (ActivityPage, error) {
	params := ParseActivityParams(q, ParseParams(q))
	f, err := r.loadFrame(ctx, now, Params{Project: params.Project, Feature: params.Feature})
	if err != nil {
		return ActivityPage{}, fmt.Errorf("load frame: %w", err)
	}
	params.Project, params.Feature = f.params.Project, f.params.Feature
	chrome := f.chrome
	chrome.setCurrent("")
	chrome.titled("Activity")
	chrome.Crumbs = []Crumb{{Label: "Activity"}}
	mem := r.memoryChart(ctx, params, q, now)
	records, err := r.store.IntentHistory(ctx, now.Add(-activityRetention))
	if err != nil {
		page := NewActivityPage(chrome, params, nil, nil, now)
		page.Memory = mem
		page.readErr = fmt.Sprintf("Could not load activity: %v", err)
		return page, nil
	}
	tickets := make(map[plan.TicketURL]Row, len(f.rows))
	for _, row := range f.rows {
		tickets[plan.TicketURL(row.URL)] = row
	}
	var rows []ActivityRow
	for _, rec := range records {
		ticket, known := tickets[rec.Ticket]
		outsideProject := params.Project != "" && ticket.Project != params.Project
		outsideFeature := params.Feature != "" && ticket.Feature != params.Feature
		if outsideProject || outsideFeature {
			continue
		}
		run, err := r.intentRun(ctx, rec, f.in.Runs)
		if err != nil {
			return ActivityPage{}, err
		}
		st := plan.StatusOf(rec, run, now)
		if st.Phase != plan.PhaseFinished || st.Outcome == plan.IntentInProgress {
			continue
		}
		row := ActivityRow{
			ID: rec.ID, Queued: rec.At, Glyph: st.Glyph, Action: string(rec.Verb), Auto: rec.QueuedBy == plan.QueuedByLoop,
			Ref: ticketRef(string(rec.Ticket)), Outcome: st.Outcome, Detail: st.Detail, Trail: plan.Trail(st),
		}
		if known {
			row.Ref, row.Title, row.Feature = ticket.Repo+ticket.Ticket(), ticket.Title, ticket.Feature
			row.Session = SessionPath(ticket.URL)
		}
		if st.Ended.IsSome() {
			row.Took = st.Ended.Unwrap().Sub(rec.ConsumedAt.UnwrapOr(rec.At))
		}
		rows = append(rows, row)
	}
	slices.SortFunc(rows, func(a, b ActivityRow) int {
		return cmp.Or(b.Queued.Compare(a.Queued), cmp.Compare(b.ID, a.ID))
	})
	page := NewActivityPage(chrome, params, rows, distinctFeatures(f.tickets), now)
	page.Memory = mem
	return page, nil
}

func (r *Reader) intentRun(
	ctx context.Context, rec plan.IntentRecord, latest map[plan.TicketURL]plan.RunSummary,
) (option.Option[plan.RunSummary], error) {
	if rec.Run.IsNone() {
		return option.None[plan.RunSummary](), nil
	}
	id := rec.Run.Unwrap()
	if s, ok := latest[rec.Ticket]; ok && s.ID == id {
		return option.Some(s), nil
	}
	d, err := r.store.RunDetails(ctx, id)
	if err != nil {
		return option.None[plan.RunSummary](), fmt.Errorf("load run of intent %d: %w", rec.ID, err)
	}
	return option.Some(plan.RunSummary{ID: id, Outcome: d.Outcome, EndedAt: rec.FinishedAt}), nil
}

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
