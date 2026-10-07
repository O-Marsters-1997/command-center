package tracker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type githubSource struct {
	owner, repo string
	run         func(ctx context.Context, args ...string) ([]byte, error)
}

func newGithubSource(owner, repo string) *githubSource {
	return &githubSource{owner: owner, repo: repo, run: runGH}
}

func (s *githubSource) nwo() string { return s.owner + "/" + s.repo }

func (s *githubSource) Features(ctx context.Context) ([]Feature, error) {
	out, err := s.run(ctx, "label", "list", "-R", s.nwo(), "--json", "name", "--limit", "100")
	if err != nil {
		return nil, err
	}
	return decodeFeatures(out)
}

func (s *githubSource) Tickets(ctx context.Context, feature string) ([]Ticket, error) {
	out, err := s.run(ctx, "issue", "list", "-R", s.nwo(), "--state", "open",
		"--label", feature, "--json", "number,title,body,url,labels", "--limit", "100")
	if err != nil {
		return nil, err
	}
	issues, err := decodeIssues(out)
	if err != nil {
		return nil, err
	}

	tickets := make([]Ticket, 0, len(issues))
	for _, issue := range issues {
		blockedBy, err := s.blockedBy(ctx, issue.Number)
		if err != nil {
			return nil, err
		}
		tickets = append(tickets, Ticket{
			URL:       issue.URL,
			Number:    issue.Number,
			Title:     issue.Title,
			Body:      issue.Body,
			Status:    ticketStatus(issue.Labels),
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

type rawLabel struct {
	Name string `json:"name"`
}

func decodeFeatures(raw []byte) ([]Feature, error) {
	var decoded []rawLabel
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("unmarshal label list: %w", err)
	}
	features := make([]Feature, 0, len(decoded))
	for _, label := range decoded {
		if strings.HasPrefix(label.Name, "project:") {
			features = append(features, Feature(label.Name))
		}
	}
	return features, nil
}

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

func ticketStatus(labels []rawLabel) string {
	for _, label := range labels {
		if status, ok := strings.CutPrefix(label.Name, "status:"); ok {
			return status
		}
	}
	return ""
}

type rawDependency struct {
	HTMLURL string `json:"html_url"`
	State   string `json:"state"`
}

func decodeBlockedBy(raw []byte) ([]string, error) {
	var decoded []rawDependency
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("unmarshal blocked_by: %w", err)
	}
	urls := make([]string, 0, len(decoded))
	for _, dep := range decoded {
		if dep.State == "closed" {
			continue
		}
		urls = append(urls, dep.HTMLURL)
	}
	return urls, nil
}
