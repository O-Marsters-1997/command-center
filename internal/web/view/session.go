package view

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

// ErrSessionNotFound is returned when no tracked ticket matches the session route.
var ErrSessionNotFound = errors.New("no such session")

// Session is the page for one ticket's latest run.
type SessionPage struct {
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
	ResetComposer  bool

	PendingMessages []string
	Model           string
}

func (s SessionPage) Empty() bool {
	return !slices.ContainsFunc(s.Turns, func(t Turn) bool { return len(t.Log.Phases) > 0 })
}

func (s SessionPage) Running() bool { return s.Row.State == plan.Running.String() }

func (s SessionPage) AwaitingInput() bool { return s.Row.State == plan.AwaitingInput.String() }

// PendingVerb reports whether a verb is queued and not yet consumed by the loop.
func (s SessionPage) PendingVerb(verb plan.Verb) bool {
	return slices.Contains(s.Row.PendingVerbs, verb)
}

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

type SessionRef struct {
	Owner string
	Name  string
	N     int
}

func ParseSessionRef(ticketURL plan.TicketURL) (SessionRef, error) {
	parts := strings.Split(strings.TrimSuffix(string(ticketURL), "/"), "/")
	if len(parts) < 5 || parts[len(parts)-2] != "issues" {
		return SessionRef{}, fmt.Errorf("parse session ref %q: not an owner/name/issues/n URL", ticketURL)
	}
	n, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return SessionRef{}, fmt.Errorf("parse session ref %q: %w", ticketURL, err)
	}
	return SessionRef{Owner: parts[len(parts)-4], Name: parts[len(parts)-3], N: n}, nil
}

func (r SessionRef) Path() string {
	return "/s/" + r.Owner + "/" + r.Name + "/" + strconv.Itoa(r.N)
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

// Session derives the session page for /s/{owner}/{name}/{n}.
func (r *Reader) Session(ctx context.Context, now time.Time, ref SessionRef, raw bool) (SessionPage, error) {
	tickets, err := r.store.Tickets(ctx)
	if err != nil {
		return SessionPage{}, fmt.Errorf("load tickets: %w", err)
	}
	suffix := "/" + ref.Owner + "/" + ref.Name + "/issues/" + strconv.Itoa(ref.N)
	ticketURL := ""
	for _, t := range tickets {
		if strings.HasSuffix(t.URL, suffix) {
			ticketURL = t.URL
			break
		}
	}
	if ticketURL == "" {
		return SessionPage{}, ErrSessionNotFound
	}

	board, err := r.Board(ctx, now, Params{Sel: ticketURL, Log: "all"})
	if err != nil {
		return SessionPage{}, fmt.Errorf("load board: %w", err)
	}
	row, ok := board.Row(ticketURL)
	if !ok {
		return SessionPage{}, ErrSessionNotFound
	}

	intents, err := r.store.PendingVerbIntents(ctx, plan.VerbFollowUp)
	if err != nil {
		return SessionPage{}, fmt.Errorf("load pending verb intents: %w", err)
	}
	var pendingMessages []string
	for _, intent := range intents {
		if intent.TicketID == ticketURL {
			pendingMessages = append(pendingMessages, intent.Payload)
		}
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
		return SessionPage{}, fmt.Errorf("load session runs for ticket: %w", err)
	}
	turns := []Turn{{Log: row.Log}}
	if !raw {
		if turns, err = r.turns(row, runs); err != nil {
			return SessionPage{}, fmt.Errorf("build turns: %w", err)
		}
	}
	promptLog := row.LogPath
	if len(runs) > 0 {
		promptLog = runs[0].LogPath
	}
	prompt, err := readPrompt(promptLog)
	if err != nil {
		return SessionPage{}, fmt.Errorf("read prompt: %w", err)
	}
	rawText, err := rawLog(raw, row.LogPath)
	if err != nil {
		return SessionPage{}, fmt.Errorf("load raw log: %w", err)
	}
	return SessionPage{
		Turns:          turns,
		ActionsPath:    self + "?part=actions",
		PRURL:          prURL,
		Chrome:         chrome,
		Row:            row,
		Stack:          buildStack(board, row),
		Ref:            ticketRef(ticketURL),
		Prompt:         prompt,
		Raw:            raw,
		RawText:        rawText,
		TranscriptPath: self,
		RawPath:        self + "?log=raw",

		PendingMessages: pendingMessages,
		Model:           config.AgentModel,
	}, nil
}

func (r *Reader) turns(row Row, runs []store.SessionRun) ([]Turn, error) {
	if len(runs) < 2 {
		return []Turn{{Log: row.Log, Ask: row.Question}}, nil
	}
	turns := make([]Turn, len(runs))
	for i, run := range runs {
		turns[i].Log = row.Log
		turns[i].Ask = run.Question
		if i != len(runs)-1 || run.LogPath != row.LogPath {
			log, err := buildLogDetail(r.renderLine, logSource{Path: run.LogPath, TicketURL: row.URL}, Params{Log: "all"})
			if err != nil {
				return nil, fmt.Errorf("build log of run %d: %w", run.ID, err)
			}
			turns[i].Log = log
			turns[i].Log.StreamPath = ""
		}
		if i > 0 {
			turns[i].You = run.Message
		}
	}
	return turns, nil
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
	return Prompt{Text: string(data), Kept: true}, nil
}

func rawLog(raw bool, logPath string) (string, error) {
	if !raw || logPath == "" {
		return "", nil
	}
	data, err := os.ReadFile(logPath)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read log %s: %w", logPath, err)
	}
	return string(data), nil
}

// ActionVerbs is the state's verbs minus launch, which has its own modal, and follow-up, which
// is the composer.
func (s SessionPage) ActionVerbs() []plan.Verb {
	return slices.DeleteFunc(slices.Clone(s.Row.Verbs), func(v plan.Verb) bool {
		return v == plan.VerbLaunch || v == plan.VerbFollowUp
	})
}
