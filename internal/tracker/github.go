package tracker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// inFlightStatuses are the status:* labels the ticket-tracker skill's state machine ranks at
// status:ready or beyond; status:backlog and an unlabelled issue are absent.
var inFlightStatuses = map[string]bool{
	"ready":       true,
	"in-progress": true,
	"in-review":   true,
	"done":        true,
}

// githubSource reads one GitHub repo's issues through the gh CLI, exactly as internal/gh does.
type githubSource struct {
	owner, repo string
	run         func(ctx context.Context, args ...string) ([]byte, error)
}

func newGithubSource(owner, repo string) *githubSource {
	return &githubSource{owner: owner, repo: repo, run: runGH}
}

func (s *githubSource) nwo() string { return s.owner + "/" + s.repo }

func (s *githubSource) Groups(ctx context.Context) ([]Group, error) {
	out, err := s.run(ctx, "label", "list", "-R", s.nwo(), "--json", "name", "--limit", "100")
	if err != nil {
		return nil, err
	}
	return decodeGroups(out)
}

func (s *githubSource) Tickets(ctx context.Context, group string) ([]Ticket, error) {
	out, err := s.run(ctx, "issue", "list", "-R", s.nwo(), "--state", "open",
		"--label", group, "--json", "number,title,body,url,labels", "--limit", "100")
	if err != nil {
		return nil, err
	}
	issues, err := decodeIssues(out)
	if err != nil {
		return nil, err
	}

	tickets := make([]Ticket, 0, len(issues))
	for _, issue := range issues {
		status, ok := inFlightStatus(issue.Labels)
		if !ok {
			continue
		}
		blockedBy, err := s.blockedBy(ctx, issue.Number)
		if err != nil {
			return nil, err
		}
		tickets = append(tickets, Ticket{
			URL:       issue.URL,
			Number:    issue.Number,
			Title:     issue.Title,
			Body:      issue.Body,
			Status:    status,
			BlockedBy: blockedBy,
		})
	}
	return tickets, nil
}

func (s *githubSource) blockedBy(ctx context.Context, number int) ([]string, error) {
	out, err := s.run(ctx, "api", fmt.Sprintf("repos/%s/issues/%d/dependencies/blocked_by", s.nwo(), number))
	if err != nil {
		return nil, err
	}
	return decodeBlockedBy(out)
}

// IssueBody reads ticketURL's body. It needs no checkout: gh resolves a full issue URL on its
// own.
func IssueBody(ctx context.Context, ticketURL string) (string, error) {
	out, err := runGH(ctx, "issue", "view", ticketURL, "--json", "body", "--jq", ".body")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func runGH(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh %s: %w: %s", args[0], err, bytes.TrimSpace(stderr.Bytes()))
	}
	return out, nil
}

// rawLabel mirrors gh label list's JSON exactly.
type rawLabel struct {
	Name string `json:"name"`
}

func decodeGroups(raw []byte) ([]Group, error) {
	var decoded []rawLabel
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("unmarshal label list: %w", err)
	}
	groups := make([]Group, 0, len(decoded))
	for _, label := range decoded {
		if strings.HasPrefix(label.Name, "project:") {
			groups = append(groups, Group(label.Name))
		}
	}
	return groups, nil
}

// rawIssue mirrors gh issue list's JSON exactly.
type rawIssue struct {
	Number int        `json:"number"`
	Title  string     `json:"title"`
	Body   string     `json:"body"`
	URL    string     `json:"url"`
	Labels []rawLabel `json:"labels"`
}

func decodeIssues(raw []byte) ([]rawIssue, error) {
	var decoded []rawIssue
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("unmarshal issue list: %w", err)
	}
	return decoded, nil
}

func inFlightStatus(labels []rawLabel) (string, bool) {
	for _, label := range labels {
		status, ok := strings.CutPrefix(label.Name, "status:")
		if !ok {
			continue
		}
		if inFlightStatuses[status] {
			return status, true
		}
	}
	return "", false
}

// rawDependency mirrors the fields this package reads from GitHub's
// GET /repos/{owner}/{repo}/issues/{number}/dependencies/blocked_by.
type rawDependency struct {
	HTMLURL string `json:"html_url"`
}

func decodeBlockedBy(raw []byte) ([]string, error) {
	var decoded []rawDependency
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("unmarshal blocked_by: %w", err)
	}
	urls := make([]string, 0, len(decoded))
	for _, dep := range decoded {
		urls = append(urls, dep.HTMLURL)
	}
	return urls, nil
}
