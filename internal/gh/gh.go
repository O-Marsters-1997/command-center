// Package gh is the only place that knows the gh CLI's JSON shape. It normalises a pull
// request's status check rollup before anything else sees it.
package gh

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/command"
)

// PRState is a pull request's state. The zero value is Absent: absence is a value, not a
// nil pointer.
type PRState int

const (
	Absent PRState = iota
	Open
	Merged
	Closed
)

func (s PRState) String() string {
	switch s {
	case Open:
		return "open"
	case Merged:
		return "merged"
	case Closed:
		return "closed"
	case Absent:
		return "absent"
	default:
		return "absent"
	}
}

// CheckState is one gating check, after CheckRun and StatusContext have been collapsed.
type CheckState struct {
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	Conclusion string    `json:"conclusion"`
	DetailsURL string    `json:"details_url"`
	StartedAt  time.Time `json:"started_at"`
}

// PR is one pull request with its rollup reduced to the latest completed run per check name.
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
	// MergedAt is GitHub's own merge timestamp, zero unless State is Merged.
	MergedAt time.Time `json:"merged_at"`
}

// Snapshot is the tracked branches' pull requests, keyed by head branch.
type Snapshot struct {
	ByBranch map[string]PR `json:"by_branch"`
}

// gh pr list's own defaults (--state open --limit 30) would hide merged PRs and truncate.
const bulkFields = "number,headRefName,headRefOid,baseRefName,baseRefOid,isDraft,state," +
	"statusCheckRollup,author,labels,mergedAt"

const fallbackFields = "number,state,baseRefName,headRefOid,labels,mergedAt"

// List reads the pull requests for the tracked branches of the repo checked out at repoPath:
// one bulk read, then one fallback read per tracked branch the bulk read did not cover.
func (CLI) List(ctx context.Context, repoPath string, tracked []string) (Snapshot, error) {
	out, err := run(ctx, repoPath, "pr", "list", "--state", "open", "--limit", "100", "--json", bulkFields)
	if err != nil {
		return Snapshot{}, err
	}
	prs, err := decode(out)
	if err != nil {
		return Snapshot{}, fmt.Errorf("decode pr list for %s: %w", repoPath, err)
	}

	byBranch := make(map[string]PR, len(tracked))
	for _, pr := range prs {
		byBranch[pr.HeadRef] = pr
	}

	for _, branch := range tracked {
		if _, ok := byBranch[branch]; ok {
			continue
		}
		out, err := run(ctx, repoPath, "pr", "list", "--state", "all", "--head", branch,
			"--limit", "1", "--json", fallbackFields)
		if err != nil {
			return Snapshot{}, err
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

// Forge is every GitHub call the reconcile loop and its verbs make. CLI is the real one; a test
// substitutes its own.
type Forge interface {
	List(ctx context.Context, repoPath string, tracked []string) (Snapshot, error)
	IssueTitles(ctx context.Context, repoPath string) (map[string]string, error)
	Create(ctx context.Context, repoPath, base, body string, draft bool) error
	Ready(ctx context.Context, repoPath, branch string) error
	Edit(ctx context.Context, repoPath, branch, base string) error
	CloseIssue(ctx context.Context, repoPath, issueURL string) error
}

// CLI is the Forge that shells out to the gh binary.
type CLI struct{}

// Create opens a pull request for the branch checked out at repoPath against base, applying the
// keep-open label that defuses both repos' 14-day auto-close. A non-empty body overrides
// --fill's body.
func (CLI) Create(ctx context.Context, repoPath, base, body string, draft bool) error {
	args := []string{"pr", "create", "--base", base, "--fill", "--label", "keep-open"}
	if body != "" {
		args = append(args, "--body", body)
	}
	if draft {
		args = append(args, "--draft")
	}
	_, err := run(ctx, repoPath, args...)
	return err
}

// Ready marks branch's pull request as ready for review, undoing draft state.
func (CLI) Ready(ctx context.Context, repoPath, branch string) error {
	_, err := run(ctx, repoPath, "pr", "ready", branch)
	return err
}

// Edit re-points branch's pull request at base. It is idempotent: GitHub's own
// delete-branch-on-merge retarget may have got there first.
func (CLI) Edit(ctx context.Context, repoPath, branch, base string) error {
	_, err := run(ctx, repoPath, "pr", "edit", branch, "--base", base)
	return err
}

// CloseIssue closes issueURL's GitHub issue.
func (CLI) CloseIssue(ctx context.Context, repoPath, issueURL string) error {
	_, err := run(ctx, repoPath, "issue", "close", issueURL)
	return err
}

// IssueTitles reads the open issues of the repo checked out at repoPath, keyed by issue URL.
func (CLI) IssueTitles(ctx context.Context, repoPath string) (map[string]string, error) {
	out, err := run(ctx, repoPath, "issue", "list", "--json", "number,title,url", "--limit", "100")
	if err != nil {
		return nil, err
	}
	titles, err := decodeIssueTitles(out)
	if err != nil {
		return nil, fmt.Errorf("decode issue list for %s: %w", repoPath, err)
	}
	return titles, nil
}

func decodeIssueTitles(raw []byte) (map[string]string, error) {
	var decoded []rawIssue
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("unmarshal issue list: %w", err)
	}
	titles := make(map[string]string, len(decoded))
	for _, issue := range decoded {
		titles[issue.URL] = issue.Title
	}
	return titles, nil
}

func run(ctx context.Context, repoPath string, args ...string) ([]byte, error) {
	return command.Output(ctx, repoPath, "gh", args...)
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
	Typename   string `json:"__typename"`
	Name       string `json:"name"`
	Context    string `json:"context"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	State      string `json:"state"`
	DetailsURL string `json:"detailsUrl"`
	TargetURL  string `json:"targetUrl"`
	StartedAt  string `json:"startedAt"`
}

func decode(raw []byte) ([]PR, error) {
	var decoded []rawPR
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("unmarshal pr list: %w", err)
	}

	prs := make([]PR, 0, len(decoded))
	for _, r := range decoded {
		labels := make([]string, 0, len(r.Labels))
		for _, l := range r.Labels {
			labels = append(labels, l.Name)
		}
		prs = append(prs, PR{
			Number:      r.Number,
			HeadRef:     r.HeadRefName,
			HeadOid:     r.HeadRefOid,
			BaseRef:     r.BaseRefName,
			BaseOid:     r.BaseRefOid,
			AuthorLogin: r.Author.Login,
			IsDraft:     r.IsDraft,
			State:       parseState(r.State),
			Checks:      normalise(r.StatusCheckRollup),
			Labels:      labels,
			MergedAt:    parseMergedAt(r.MergedAt),
		})
	}
	return prs, nil
}

func parseMergedAt(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func parseState(s string) PRState {
	switch s {
	case "OPEN":
		return Open
	case "MERGED":
		return Merged
	case "CLOSED":
		return Closed
	default:
		return Absent
	}
}

// normalise reduces the rollup to the latest completed run per check name. gh's rollup repeats
// a name several times at one head SHA, and StatusContext entries can carry no name at all.
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

	started, err := time.Parse(time.RFC3339, r.StartedAt)
	if err != nil {
		started = time.Time{}
	}
	return CheckState{Name: name, Status: status, Conclusion: conclusion, DetailsURL: url, StartedAt: started}, true
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
