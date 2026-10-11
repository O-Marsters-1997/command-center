package view

import (
	"context"
	"errors"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

// ErrSessionNotFound is returned when no tracked ticket matches the session route.
var ErrSessionNotFound = errors.New("no such session")

// Session is the page for one ticket's latest run.
type Session struct {
	Chrome
	Row            Row
	Stack          Stack
	Ref            string
	Prompt         Prompt
	Turns          []Turn
	Raw            bool
	RawText        string
	TranscriptPath string
	RawPath        string
	ActionsPath    string
	PRURL          string
	Toast          string
}

func (s Session) Empty() bool {
	return !slices.ContainsFunc(s.Turns, func(t Turn) bool { return len(t.Log.Phases) > 0 })
}

// Running reports whether an agent is live on the ticket, so the composer shows its status line
// and Kill instead of a follow-up box.
func (s Session) Running() bool { return s.Row.State == plan.Running.String() }

func (s Session) AwaitingInput() bool { return s.Row.State == plan.AwaitingInput.String() }

// PendingVerb reports whether a verb is queued and not yet consumed by the loop.
func (s Session) PendingVerb(verb string) bool { return slices.Contains(s.Row.PendingVerbs, verb) }

type Turn struct {
	You string
	Log LogDetail
	Ask string
}

// Prompt is the run's own prompt file. Kept is false when the file is gone.
type Prompt struct {
	Text string
	Kept bool
}

// SessionPath is the route for a ticket URL's session page, or "" when the URL is not a
// {owner}/{name}/issues/{n} GitHub URL.
func SessionPath(ticketURL string) string {
	parts := strings.Split(strings.TrimSuffix(ticketURL, "/"), "/")
	if len(parts) < 5 || parts[len(parts)-2] != "issues" {
		return ""
	}
	return "/s/" + parts[len(parts)-4] + "/" + parts[len(parts)-3] + "/" + parts[len(parts)-1]
}

// Session derives the session page for /s/{owner}/{name}/{n}.
func (r *Reader) Session(ctx context.Context, now time.Time, owner, name, n string, raw bool) (Session, error) {
	tickets, err := r.store.Tickets(ctx)
	if err != nil {
		return Session{}, err
	}
	suffix := "/" + owner + "/" + name + "/issues/" + n
	ticketURL := ""
	for _, t := range tickets {
		if strings.HasSuffix(t.URL, suffix) {
			ticketURL = t.URL
			break
		}
	}
	if ticketURL == "" {
		return Session{}, ErrSessionNotFound
	}

	board, err := r.Board(ctx, now, Params{Sel: ticketURL, Log: "all"})
	if err != nil {
		return Session{}, err
	}
	row, ok := board.Row(ticketURL)
	if !ok {
		return Session{}, ErrSessionNotFound
	}

	self := SessionPath(ticketURL)
	chrome := board.Chrome
	chrome.Section = "tickets"
	chrome.Home = false
	prURL := ""
	if row.PRNumber > 0 {
		prURL = "https://github.com/" + row.Repo + "/pull/" + strconv.Itoa(row.PRNumber)
	}
	runs, err := r.store.SessionRunsForTicket(ctx, ticketURL)
	if err != nil {
		return Session{}, err
	}
	turns := []Turn{{Log: row.Log}}
	if !raw {
		turns = r.turns(row, runs)
	}
	promptLog := row.LogPath
	if len(runs) > 0 {
		promptLog = runs[0].LogPath
	}
	return Session{
		Turns:          turns,
		ActionsPath:    self + "?part=actions",
		PRURL:          prURL,
		Chrome:         chrome,
		Row:            row,
		Stack:          buildStack(board, row),
		Ref:            ticketRef(ticketURL),
		Prompt:         readPrompt(promptLog),
		Raw:            raw,
		RawText:        rawLog(raw, row.LogPath),
		TranscriptPath: self,
		RawPath:        self + "?log=raw",
	}, nil
}

func (r *Reader) turns(row Row, runs []store.SessionRun) []Turn {
	if len(runs) < 2 {
		return []Turn{{Log: row.Log, Ask: row.Question}}
	}
	turns := make([]Turn, len(runs))
	for i, run := range runs {
		turns[i].Log = row.Log
		turns[i].Ask = run.Question
		if i != len(runs)-1 || run.LogPath != row.LogPath {
			turns[i].Log = buildLogDetail(r.renderLine, run.LogPath, false, row.URL, nil, Params{Log: "all"})
			turns[i].Log.StreamPath = ""
		}
		if i > 0 {
			turns[i].You = run.Message
		}
	}
	return turns
}

func readPrompt(logPath string) Prompt {
	if logPath == "" {
		return Prompt{}
	}
	data, err := os.ReadFile(strings.TrimSuffix(logPath, path.Ext(logPath)) + ".prompt")
	if err != nil {
		return Prompt{}
	}
	return Prompt{Text: string(data), Kept: true}
}

func rawLog(raw bool, logPath string) string {
	if !raw || logPath == "" {
		return ""
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		return ""
	}
	return string(data)
}

// ActionVerbs is the state's verbs minus launch, which has its own modal, and follow-up, which
// is the composer.
func (s Session) ActionVerbs() []string {
	return slices.DeleteFunc(slices.Clone(s.Row.Verbs), func(v string) bool {
		return v == plan.VerbLaunch || v == plan.VerbFollowUp
	})
}
