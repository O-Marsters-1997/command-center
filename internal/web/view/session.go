package view

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	"github.com/O-Marsters-1997/command-center/internal/plan/spend"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

// ErrSessionNotFound is returned when no tracked ticket matches the session route.
var ErrSessionNotFound = errors.New("view: no such session")

type SessionPage struct {
	Chrome
	Row            Row
	Stack          Stack
	Ref            string
	Prompt         Prompt
	Turns          []Turn
	Raw            bool
	RawLog         RawLog
	TranscriptPath string
	RawPath        string
	ChangesPath    string
	ActionsPath    string
	Actions        []VerbButton
	Callout        bool
	PRURL          string
	Toast          string
	UndoID         int64
	UndoVerb       plan.Verb
	ResetComposer  bool
	Fragment       bool

	PendingMessages []string
	Model           string
	Models          []string
	Efforts         []plan.Effort
	Launched        option.Option[Age]
	Worked          option.Option[WorkedSummary]
	Steps           []StepCell
	Selected        option.Option[StepCell]
	stepsPath       string
}

func (s SessionPage) StepsPath() string { return s.stepsPath }

type StepCell struct {
	Step    string
	Round   int
	Glyph   plan.GlyphWord
	Model   string
	Wall    string
	PctWeek float64
	Turns   int
	Live    bool
	RunID   plan.RunID
}

func (c StepCell) Label() string {
	if c.Round > 0 {
		return c.Step + " " + strconv.Itoa(c.Round)
	}
	return c.Step
}

func (s SessionPage) StepPath(c StepCell) string {
	return s.TranscriptPath + "?run=" + strconv.FormatInt(int64(c.RunID), 10)
}

func (s SessionPage) StepLive() bool {
	return slices.ContainsFunc(s.Steps, func(c StepCell) bool { return c.Live })
}

func (s SessionPage) IsSelected(c StepCell) bool {
	return s.Selected.IsSome() && s.Selected.Unwrap().RunID == c.RunID
}

const fixStep plan.StepName = "fix"

type stepInputs struct {
	live   option.Option[WorkedSummary]
	alive  bool
	factor float64
	pick   plan.RunID
}

func buildSteps(runs []store.SessionRun, in stepInputs) ([]StepCell, option.Option[StepCell]) {
	cells := make([]StepCell, len(runs))
	fixes := 0
	for i, r := range runs {
		cell := StepCell{Step: string(r.Step), Model: r.Model, RunID: plan.RunID(r.ID), Glyph: plan.GlyphPending}
		if r.Step == fixStep {
			fixes++
			cell.Round = fixes
		}
		switch outcome, err := plan.ParseOutcome(r.Outcome); {
		case i == len(runs)-1 && in.alive:
			cell.Glyph, cell.Live = plan.GlyphRunning, true
			if in.live.IsSome() {
				cell.Wall, cell.Turns = in.live.Unwrap().Wall, in.live.Unwrap().Turns
			}
		case err == nil:
			cell.Glyph = outcome.Glyph()
		}
		if r.Metrics.IsSome() && !cell.Live {
			m := r.Metrics.Unwrap()
			cell.Turns = m.Turns
			if m.Wall.IsSome() {
				cell.Wall = formatShort(m.Wall.Unwrap())
			}
		}
		if r.Cost.IsSome() {
			cell.PctWeek = spend.PctWeek(r.Cost.Unwrap(), in.factor)
		}
		cells[i] = cell
	}
	if len(cells) == 0 {
		return nil, option.None[StepCell]()
	}
	selected := cells[len(cells)-1]
	if i := slices.IndexFunc(cells, func(c StepCell) bool { return c.RunID == in.pick }); i >= 0 {
		selected = cells[i]
	}
	return cells, option.Some(selected)
}

func (s SessionPage) Empty() bool {
	return !slices.ContainsFunc(s.Turns, func(t Turn) bool { return len(t.Log.Phases) > 0 })
}

func (s SessionPage) Running() bool { return s.Row.State == plan.Running.String() }

func (s SessionPage) AwaitingInput() bool { return s.Row.State == plan.AwaitingInput.String() }

type Turn struct {
	You string
	Log LogDetail
	Ask string
}

type Prompt struct {
	Text string
	Lead string
	Kept bool
}

type SessionRef struct {
	Owner string
	Name  string
	N     string
	Run   plan.RunID
}

func ParseSessionRef(ticketURL plan.TicketURL) (SessionRef, error) {
	parts := strings.Split(strings.TrimSuffix(string(ticketURL), "/"), "/")
	if len(parts) < 5 || parts[len(parts)-2] != "issues" {
		return SessionRef{}, fmt.Errorf("parse session ref %q: not an owner/name/issues/n URL", ticketURL)
	}
	return SessionRef{Owner: parts[len(parts)-4], Name: parts[len(parts)-3], N: parts[len(parts)-1]}, nil
}

func (r SessionRef) pathWithRun() string {
	if r.Run == 0 {
		return r.Path()
	}
	return r.Path() + "?run=" + strconv.FormatInt(int64(r.Run), 10)
}

func (r SessionRef) Path() string {
	return "/s/" + r.Owner + "/" + r.Name + "/" + r.N
}

// SessionPath is the route for a ticket URL's session page, or "" when the URL is not a
// {owner}/{name}/issues/{n} GitHub URL.
func SessionPath(ticketURL string) string {
	ref, err := ParseSessionRef(plan.TicketURL(ticketURL))
	if err != nil {
		return ""
	}
	return ref.Path()
}

func sessionCrumbs(row Row) []Crumb {
	var crumbs []Crumb
	if row.Project != "" {
		crumbs = append(crumbs, projectCrumb(row.Mark()))
	}
	if row.Feature != "" {
		crumbs = append(crumbs, Crumb{Label: row.Feature, Path: "/f/" + url.PathEscape(row.Feature)})
	}
	return append(crumbs, Crumb{Label: cmp.Or(row.Title, row.Ticket())})
}

type RawFilter struct {
	Type  string
	Query string
}

type RawLog struct {
	Filter RawFilter
	Types  []string
	Lines  []agentlog.RawLine
	Total  int
}

func (r *Reader) Session(
	ctx context.Context, now time.Time, ref SessionRef, raw option.Option[RawFilter],
) (SessionPage, error) {
	board, err := r.Board(ctx, now, Params{})
	if err != nil {
		return SessionPage{}, fmt.Errorf("load board: %w", err)
	}
	row, ok := board.rowFor(ref)
	if !ok {
		return SessionPage{}, ErrSessionNotFound
	}
	ticketURL := row.URL

	intents, err := r.store.PendingVerbIntents(ctx, plan.VerbFollowUp)
	if err != nil {
		return SessionPage{}, fmt.Errorf("load pending verb intents: %w", err)
	}
	var pendingMessages []string
	for _, intent := range intents {
		if intent.TicketID == ticketURL {
			pendingMessages = append(pendingMessages, store.DecodeFollowUpPayload(intent.Payload).Message)
		}
	}
	snap, err := r.Snapshot(ctx, now)
	if err != nil {
		return SessionPage{}, fmt.Errorf("load snapshot: %w", err)
	}
	entry, _ := snap.Entry(plan.TicketURL(ticketURL))
	model := cmp.Or(entry.Ticket.Choice.Model, config.AgentModel)
	models := config.AgentModels()
	if !slices.Contains(models, model) {
		models = append(models, model)
	}

	self := ref.Path()
	chrome := board.Chrome
	chrome.Home = false
	chrome.titled(row.Ticket())
	chrome.Crumbs = sessionCrumbs(row)
	prURL := ""
	if row.PRNumber > 0 {
		prURL = "https://github.com/" + row.Repo + "/pull/" + strconv.Itoa(row.PRNumber)
	}
	actions := slices.Clone(row.Buttons)
	for i := range actions {
		actions[i].Label = string(actions[i].Verb)
	}
	runs, err := r.store.SessionRunsForTicket(ctx, ticketURL)
	if err != nil {
		return SessionPage{}, fmt.Errorf("load session runs for ticket: %w", err)
	}
	var records []store.Event
	err = EachTicketEvent(ctx, r.store, ticketURL, 0, func(e store.Event) error {
		records = append(records, e)
		return nil
	})
	if err != nil {
		return SessionPage{}, fmt.Errorf("load ticket events: %w", err)
	}
	current := LogSource{
		Path: row.LogPath, Streaming: row.Alive, TicketURL: ticketURL, Records: records, Checks: row.Checks,
	}
	row.Log = BuildLogDetail(current, Params{Log: LogAll})
	if len(runs) > 0 && row.Log.PhaseCount == 0 {
		if stats, ok := storedStats(runs[len(runs)-1].Metrics); ok {
			row.Log.Worked = stats.worked()
			row.Log.Summary = WorkedSummary{Wall: formatShort(stats.wall), Turns: stats.turns}
		}
	}
	worked := option.None[WorkedSummary]()
	if row.Log.Worked.Label != "" {
		summary := row.Log.Summary
		summary.SpendPctWeek = row.SpendPctWeek
		worked = option.Some(summary)
	}
	turns := []Turn{{Log: row.Log}}
	if raw.IsNone() {
		turns = buildTurns(row, runs)
	}
	fits, err := r.store.FitFactors(ctx, now)
	if err != nil {
		return SessionPage{}, fmt.Errorf("load fit factors: %w", err)
	}
	steps, selected := buildSteps(runs, stepInputs{
		live: worked, alive: row.Alive, factor: fits[agentlog.SevenDay].Factor, pick: ref.Run,
	})
	picked := slices.IndexFunc(runs, func(run store.SessionRun) bool { return plan.RunID(run.ID) == ref.Run })
	if picked >= 0 && raw.IsNone() {
		turns = turns[picked : picked+1]
	}
	promptLog := row.LogPath
	if len(runs) > 0 {
		promptLog = runs[0].LogPath
	}
	prompt, err := readPrompt(promptLog)
	if err != nil {
		return SessionPage{}, fmt.Errorf("read prompt: %w", err)
	}
	if !prompt.Kept && len(runs) > 0 && runs[0].Prompt != "" {
		prompt = keptPrompt(runs[0].Prompt)
	}
	rawLog, err := readRawLog(raw, row.LogPath)
	if err != nil {
		return SessionPage{}, fmt.Errorf("load raw log: %w", err)
	}
	launched := option.None[Age]()
	if row.Feature != "" {
		at, err := r.store.FeatureFirstLaunch(ctx, row.Feature)
		switch {
		case err == nil:
			launched = option.Some(relative(now, at))
		case !errors.Is(err, store.ErrNotFound):
			return SessionPage{}, fmt.Errorf("load feature first launch: %w", err)
		}
	}
	return SessionPage{
		Launched:       launched,
		Steps:          steps,
		Selected:       selected,
		stepsPath:      ref.pathWithRun(),
		Worked:         worked,
		Turns:          turns,
		ActionsPath:    self + "/actions",
		Actions:        actions,
		Callout:        row.Glyph == plan.GlyphFailed || row.Glyph == plan.GlyphAttention,
		PRURL:          prURL,
		Chrome:         chrome,
		Row:            row,
		Stack:          buildStack(board, row),
		Ref:            ticketRef(ticketURL),
		Prompt:         prompt,
		Raw:            raw.IsSome(),
		RawLog:         rawLog,
		TranscriptPath: self,
		RawPath:        self + "?log=raw",
		ChangesPath:    self + "/changes",

		PendingMessages: pendingMessages,
		Model:           model,
		Models:          models,
		Efforts:         plan.AllEfforts(),
	}, nil
}

func buildTurns(row Row, runs []store.SessionRun) []Turn {
	if len(runs) < 2 {
		return []Turn{{Log: row.Log, Ask: row.Question}}
	}
	turns := make([]Turn, len(runs))
	for i, run := range runs {
		turns[i].Log = row.Log
		turns[i].Ask = run.Question
		if i != len(runs)-1 || run.LogPath != row.LogPath {
			turns[i].Log = BuildLogDetail(LogSource{Path: run.LogPath, TicketURL: row.URL}, Params{Log: "all"})
			turns[i].Log.StreamPath = ""
		}
		if i > 0 {
			turns[i].You = run.Message
		}
	}
	return turns
}

func readPrompt(logPath string) (Prompt, error) {
	if logPath == "" {
		return Prompt{}, nil
	}
	data, err := os.ReadFile(strings.TrimSuffix(logPath, path.Ext(logPath)) + ".prompt")
	if errors.Is(err, fs.ErrNotExist) {
		return Prompt{}, nil
	}
	if err != nil {
		return Prompt{}, fmt.Errorf("read prompt for %s: %w", logPath, err)
	}
	return keptPrompt(string(data)), nil
}

const leadWords = 10

func keptPrompt(text string) Prompt {
	var words []string
	for line := range strings.Lines(text) {
		if words = strings.Fields(line); len(words) > 0 {
			break
		}
	}
	lead := strings.Join(words[:min(len(words), leadWords)], " ")
	if len(words) > leadWords {
		lead += "…"
	}
	return Prompt{Text: text, Lead: lead, Kept: true}
}

func readRawLog(raw option.Option[RawFilter], logPath string) (RawLog, error) {
	if raw.IsNone() {
		return RawLog{}, nil
	}
	filter := raw.Unwrap()
	if logPath == "" {
		return RawLog{Filter: filter}, nil
	}
	f, err := os.Open(logPath)
	if errors.Is(err, fs.ErrNotExist) {
		return RawLog{Filter: filter}, nil
	}
	if err != nil {
		return RawLog{}, fmt.Errorf("open log %s: %w", logPath, err)
	}
	lines, err := agentlog.ParseRaw(f)
	if err = errors.Join(err, f.Close()); err != nil {
		return RawLog{}, fmt.Errorf("parse log %s: %w", logPath, err)
	}
	return filterRaw(lines, filter), nil
}

func filterRaw(lines []agentlog.RawLine, filter RawFilter) RawLog {
	log := RawLog{Filter: filter, Total: len(lines)}
	query := strings.ToLower(filter.Query)
	for _, line := range lines {
		if !slices.Contains(log.Types, line.Type) {
			log.Types = append(log.Types, line.Type)
		}
		if filter.Type != "" && line.Type != filter.Type {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(line.Tool+"\n"+line.Summary+"\n"+line.JSON), query) {
			continue
		}
		log.Lines = append(log.Lines, line)
	}
	slices.Sort(log.Types)
	return log
}
