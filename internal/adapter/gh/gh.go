package gh

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

// PRState is a pull request's state; its zero value represents absence.
type PRState int

const (
	Absent PRState = iota
	Open
	Merged
	Closed
)

func (s PRState) String() string {
	names := [...]string{"absent", "open", "merged", "closed"}
	if s >= 0 && int(s) < len(names) {
		return names[s]
	}
	return "absent"
}

type CheckState struct {
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	Conclusion  string    `json:"conclusion"`
	DetailsURL  string    `json:"details_url"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
}

// PR is a pull request with checks reduced to the latest completed run per name.
type PR struct {
	Number      int                   `json:"number"`
	HeadRef     string                `json:"head_ref"`
	HeadOid     string                `json:"head_oid"`
	BaseRef     string                `json:"base_ref"`
	BaseOid     string                `json:"base_oid"`
	AuthorLogin string                `json:"author_login"`
	IsDraft     bool                  `json:"is_draft"`
	State       PRState               `json:"state"`
	Checks      map[string]CheckState `json:"checks"`
	Labels      []string              `json:"labels"`
	MergedAt    time.Time             `json:"merged_at"`
}

type Snapshot struct {
	ByBranch map[string]PR `json:"by_branch"`
}

const ListLimit = 100

type FullPageError struct{ Limit int }

func (e *FullPageError) Error() string {
	return fmt.Sprintf("gh list returned a full page at --limit %d, results may be truncated", e.Limit)
}

const bulkFields = "number,headRefName,headRefOid,baseRefName,baseRefOid,isDraft,state," +
	"statusCheckRollup,author,labels,mergedAt"

const fallbackFields = "number,state,baseRefName,headRefOid,labels,mergedAt"

// List reads tracked branches' pull requests, including merged and closed PRs
// omitted by GitHub's open-only bulk listing.
func (c CLI) List(ctx context.Context, repoPath string, tracked []string) (Snapshot, error) {
	out, err := c.Run(ctx, repoPath, "pr", "list", "--state", "open",
		"--limit", strconv.Itoa(ListLimit), "--json", bulkFields)
	if err != nil {
		return Snapshot{}, fmt.Errorf("list open pull requests: %w", err)
	}
	prs, err := decode(out)
	if err != nil {
		return Snapshot{}, fmt.Errorf("decode pr list for %s: %w", repoPath, err)
	}
	if len(prs) >= ListLimit {
		return Snapshot{}, fmt.Errorf("read pr list for %s: %w", repoPath, &FullPageError{Limit: ListLimit})
	}

	byBranch := make(map[string]PR, len(tracked))
	for _, pr := range prs {
		byBranch[pr.HeadRef] = pr
	}

	for _, branch := range tracked {
		if _, ok := byBranch[branch]; ok {
			continue
		}
		out, err := c.Run(ctx, repoPath, "pr", "list", "--state", "all", "--head", branch,
			"--limit", "1", "--json", fallbackFields)
		if err != nil {
			return Snapshot{}, fmt.Errorf("list pull requests for %s: %w", branch, err)
		}
		found, err := decode(out)
		if err != nil {
			return Snapshot{}, fmt.Errorf("decode pr list for %s %s: %w", repoPath, branch, err)
		}
		if len(found) == 0 {
			continue
		}
		pr := found[0]
		pr.HeadRef = branch
		byBranch[branch] = pr
	}

	return Snapshot{ByBranch: byBranch}, nil
}

type Forge interface {
	List(ctx context.Context, repoPath string, tracked []string) (Snapshot, error)
	IssueTitles(ctx context.Context, repoPath string) (map[string]string, error)
	Create(ctx context.Context, repoPath, base, body string, draft bool) error
	Ready(ctx context.Context, repoPath, branch string) error
	Edit(ctx context.Context, repoPath, branch, base string) error
	Comment(ctx context.Context, repoPath, branch, body string) error
	CloseIssue(ctx context.Context, repoPath, issueURL string) error
}

// Create opens a pull request with the keep-open label to avoid GitHub's
// configured 14-day auto-close; a non-empty body overrides --fill.
func (c CLI) Create(ctx context.Context, repoPath, base, body string, draft bool) error {
	args := []string{"pr", "create", "--base", base, "--fill", "--label", "keep-open"}
	if body != "" {
		args = append(args, "--body", body)
	}
	if draft {
		args = append(args, "--draft")
	}
	_, err := c.Run(ctx, repoPath, args...)
	if err != nil {
		return fmt.Errorf("create pull request: %w", err)
	}
	return nil
}

func (c CLI) Ready(ctx context.Context, repoPath, branch string) error {
	_, err := c.Run(ctx, repoPath, "pr", "ready", branch)
	if err != nil {
		return fmt.Errorf("ready pull request %s: %w", branch, err)
	}
	return nil
}

func (c CLI) Comment(ctx context.Context, repoPath, branch, body string) error {
	_, err := c.Run(ctx, repoPath, "pr", "comment", branch, "--body", body)
	if err != nil {
		return fmt.Errorf("comment on pull request %s: %w", branch, err)
	}
	return nil
}

// Edit re-points branch's pull request at base. It is idempotent: GitHub's own
// delete-branch-on-merge retarget may have got there first.
func (c CLI) Edit(ctx context.Context, repoPath, branch, base string) error {
	_, err := c.Run(ctx, repoPath, "pr", "edit", branch, "--base", base)
	if err != nil {
		return fmt.Errorf("edit pull request %s: %w", branch, err)
	}
	return nil
}

func (c CLI) CloseIssue(ctx context.Context, repoPath, issueURL string) error {
	_, err := c.Run(ctx, repoPath, "issue", "close", issueURL)
	if err != nil {
		return fmt.Errorf("close issue %s: %w", issueURL, err)
	}
	return nil
}

func (c CLI) IssueTitles(ctx context.Context, repoPath string) (map[string]string, error) {
	out, err := c.Run(ctx, repoPath, "issue", "list", "--json", "number,title,url", "--limit", strconv.Itoa(ListLimit))
	if err != nil {
		return nil, fmt.Errorf("list issue titles: %w", err)
	}
	titles, err := decodeIssueTitles(out)
	if err != nil {
		return nil, fmt.Errorf("decode issue list for %s: %w", repoPath, err)
	}
	return titles, nil
}

func decodeIssueTitles(raw []byte) (map[string]string, error) {
	decoded, err := Decode[[]rawIssue](raw)
	if err != nil {
		return nil, fmt.Errorf("issue list: %w", err)
	}
	if len(decoded) >= ListLimit {
		return nil, fmt.Errorf("read issues: %w", &FullPageError{Limit: ListLimit})
	}
	titles := make(map[string]string, len(decoded))
	for _, issue := range decoded {
		titles[issue.URL] = issue.Title
	}
	return titles, nil
}

type rawPR struct {
	Number      int    `json:"number"`
	HeadRefName string `json:"headRefName"`
	HeadRefOid  string `json:"headRefOid"`
	BaseRefName string `json:"baseRefName"`
	BaseRefOid  string `json:"baseRefOid"`
	IsDraft     bool   `json:"isDraft"`
	State       string `json:"state"`
	Author      struct {
		Login string `json:"login"`
	} `json:"author"`
	StatusCheckRollup []rawCheck `json:"statusCheckRollup"`
	Labels            []struct {
		Name string `json:"name"`
	} `json:"labels"`
	MergedAt string `json:"mergedAt"`
}

type rawIssue struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// rawCheck is the union of CheckRun and StatusContext as gh flattens them into one array.
type rawCheck struct {
	Typename    string `json:"__typename"`
	Name        string `json:"name"`
	Context     string `json:"context"`
	Status      string `json:"status"`
	Conclusion  string `json:"conclusion"`
	State       string `json:"state"`
	DetailsURL  string `json:"detailsUrl"`
	TargetURL   string `json:"targetUrl"`
	StartedAt   string `json:"startedAt"`
	CompletedAt string `json:"completedAt"`
}

func decode(raw []byte) ([]PR, error) {
	decoded, err := Decode[[]rawPR](raw)
	if err != nil {
		return nil, fmt.Errorf("pr list: %w", err)
	}

	prs := make([]PR, 0, len(decoded))
	for _, r := range decoded {
		labels := make([]string, 0, len(r.Labels))
		for _, l := range r.Labels {
			labels = append(labels, l.Name)
		}
		state, err := parseState(r.State)
		if err != nil {
			return nil, fmt.Errorf("decode pr %d: %w", r.Number, err)
		}
		mergedAt, err := parseMergedAt(r.MergedAt)
		if err != nil {
			return nil, fmt.Errorf("decode pr %d: %w", r.Number, err)
		}
		prs = append(prs, PR{
			Number:      r.Number,
			HeadRef:     r.HeadRefName,
			HeadOid:     r.HeadRefOid,
			BaseRef:     r.BaseRefName,
			BaseOid:     r.BaseRefOid,
			AuthorLogin: r.Author.Login,
			IsDraft:     r.IsDraft,
			State:       state,
			Checks:      normalise(r.StatusCheckRollup),
			Labels:      labels,
			MergedAt:    mergedAt,
		})
	}
	return prs, nil
}

func parseMergedAt(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse mergedAt: %w", err)
	}
	return t, nil
}

func parseState(s string) (PRState, error) {
	switch s {
	case "OPEN":
		return Open, nil
	case "MERGED":
		return Merged, nil
	case "CLOSED":
		return Closed, nil
	default:
		return Absent, fmt.Errorf("unknown pr state %q", s)
	}
}

// GitHub's rollup repeats a name at one head SHA; StatusContext can omit name.
func normalise(rollup []rawCheck) map[string]CheckState {
	checks := make(map[string]CheckState, len(rollup))
	for _, r := range rollup {
		candidate, ok := collapse(r)
		if !ok {
			continue
		}
		existing, seen := checks[candidate.Name]
		if !seen || better(candidate, existing) {
			checks[candidate.Name] = candidate
		}
	}
	return checks
}

func collapse(r rawCheck) (CheckState, bool) {
	name := r.Name
	if name == "" {
		name = r.Context
	}
	if name == "" {
		return CheckState{}, false
	}

	status, conclusion, url := r.Status, r.Conclusion, r.DetailsURL
	if r.Typename == "StatusContext" {
		status, conclusion, url = statusContextStatus(r.State), r.State, r.TargetURL
	}

	return CheckState{
		Name:        name,
		Status:      status,
		Conclusion:  conclusion,
		DetailsURL:  url,
		StartedAt:   parseTimeOrZero(r.StartedAt),
		CompletedAt: parseTimeOrZero(r.CompletedAt),
	}, true
}

func parseTimeOrZero(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func statusContextStatus(state string) string {
	switch state {
	case "SUCCESS", "FAILURE", "ERROR":
		return "COMPLETED"
	default:
		return "PENDING"
	}
}

func better(candidate, existing CheckState) bool {
	switch {
	case completed(candidate) && !completed(existing):
		return true
	case !completed(candidate) && completed(existing):
		return false
	default:
		return candidate.StartedAt.After(existing.StartedAt)
	}
}

func completed(c CheckState) bool { return c.Status == "COMPLETED" }
