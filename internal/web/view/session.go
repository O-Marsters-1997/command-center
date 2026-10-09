package view

import (
	"context"
	"errors"
	"os"
	"path"
	"strings"
	"time"
)

// ErrSessionNotFound is returned when no tracked ticket matches the session route.
var ErrSessionNotFound = errors.New("no such session")

// Session is the page for one ticket's latest run.
type Session struct {
	Chrome
	Row            Row
	Ref            string
	Prompt         Prompt
	Raw            bool
	RawText        string
	TranscriptPath string
	RawPath        string
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

	board, err := r.Board(ctx, now, Params{Sel: ticketURL, View: "board", Log: "all"})
	if err != nil {
		return Session{}, err
	}
	row, ok := board.Row(ticketURL)
	if !ok {
		return Session{}, ErrSessionNotFound
	}

	self := SessionPath(ticketURL)
	chrome := board.Chrome
	chrome.Section = "board"
	chrome.Home = false
	return Session{
		Chrome:         chrome,
		Row:            row,
		Ref:            ticketRef(ticketURL),
		Prompt:         readPrompt(row.LogPath),
		Raw:            raw,
		RawText:        rawLog(raw, row.LogPath),
		TranscriptPath: self,
		RawPath:        self + "?log=raw",
	}, nil
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
