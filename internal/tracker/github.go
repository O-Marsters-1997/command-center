package tracker

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/gh"
)

const listLimit = gh.ListLimit

type githubSource struct {
	owner, repo string
	cli         gh.CLI
}

func (s *githubSource) run(ctx context.Context, args ...string) ([]byte, error) {
	out, err := s.cli.Run(ctx, "", args...)
	if err != nil {
		return nil, fmt.Errorf("query github tracker: %w", err)
	}
	return out, nil
}

func (s *githubSource) nwo() string { return s.owner + "/" + s.repo }

func (s *githubSource) Features(ctx context.Context) ([]Feature, error) {
	out, err := s.run(ctx, "label", "list", "-R", s.nwo(), "--json", "name", "--limit", strconv.Itoa(listLimit))
	if err != nil {
		return nil, fmt.Errorf("list feature labels: %w", err)
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
	decoded, err := gh.Decode[[]rawLabel](raw)
	if err != nil {
		return nil, fmt.Errorf("label list: %w", err)
	}
	features := make([]Feature, 0, len(decoded))
	for _, label := range decoded {
		if strings.HasPrefix(label.Name, "project:") {
			features = append(features, Feature(label.Name))
		}
	}
	if len(decoded) >= listLimit {
		return nil, fmt.Errorf("read labels: %w", &gh.FullPageError{Limit: listLimit})
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
	decoded, err := gh.Decode[[]rawIssue](raw)
	if err != nil {
		return nil, fmt.Errorf("issue list: %w", err)
	}
	if len(decoded) >= listLimit {
		return nil, fmt.Errorf("read issues: %w", &gh.FullPageError{Limit: listLimit})
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
