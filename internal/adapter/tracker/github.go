package tracker

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/adapter/gh"
)

const listLimit = gh.ListLimit

type githubSource struct {
	owner, repo string
	cli         gh.CLI
}

func (s *githubSource) run(ctx context.Context, args ...string) ([]byte, error) {
	return s.cli.Read(ctx, "", args...)
}

func (s *githubSource) nwo() string { return s.owner + "/" + s.repo }

func (s *githubSource) Features(ctx context.Context) ([]Feature, error) {
	out, err := s.run(ctx, "issue", "list", "-R", s.nwo(), "--state", "open",
		"--json", "labels", "--limit", strconv.Itoa(listLimit))
	if err != nil {
		return nil, fmt.Errorf("list open issues for features: %w", err)
	}
	return decodeFeatures(out)
}

func (s *githubSource) Tickets(ctx context.Context, feature string) ([]Ticket, error) {
	out, err := s.run(ctx, "issue", "list", "-R", s.nwo(), "--state", "open",
		"--label", feature, "--json", "number,title,body,url,labels", "--limit", strconv.Itoa(listLimit))
	if err != nil {
		return nil, fmt.Errorf("list tickets for %s: %w", feature, err)
	}
	issues, err := decodeIssues(out)
	if err != nil {
		return nil, fmt.Errorf("decode tickets for %s: %w", feature, err)
	}

	tickets := make([]Ticket, 0, len(issues))
	for _, issue := range issues {
		blockedBy, err := s.blockedBy(ctx, issue.Number)
		if err != nil {
			return nil, fmt.Errorf("read blockers for issue %d: %w", issue.Number, err)
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
	out, err := s.run(ctx, "api", "--paginate", fmt.Sprintf("repos/%s/issues/%d/dependencies/blocked_by", s.nwo(), number))
	if err != nil {
		return nil, fmt.Errorf("list blockers for issue %d: %w", number, err)
	}
	return decodeBlockedBy(out)
}

type rawLabel struct {
	Name string `json:"name"`
}

func decodeFeatures(raw []byte) ([]Feature, error) {
	issues, err := decodeIssues(raw)
	if err != nil {
		return nil, fmt.Errorf("decode features: %w", err)
	}
	features := []Feature{}
	for _, issue := range issues {
		for _, label := range issue.Labels {
			f := Feature(label.Name)
			if strings.HasPrefix(label.Name, "project:") && !slices.Contains(features, f) {
				features = append(features, f)
			}
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
	decoded, err := gh.DecodePage[rawIssue](raw)
	if err != nil {
		return nil, fmt.Errorf("issue list: %w", err)
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
	deps, err := gh.DecodeStream[rawDependency](raw)
	if err != nil {
		return nil, fmt.Errorf("blocked_by: %w", err)
	}
	urls := []string{}
	for _, dep := range deps {
		if dep.State == "closed" {
			continue
		}
		urls = append(urls, dep.HTMLURL)
	}
	return urls, nil
}
