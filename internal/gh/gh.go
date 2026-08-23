// Package gh is the only place that knows the gh CLI's JSON shape. It normalises a pull
// request's status check rollup before anything else sees it (docs/command-centre-v1.md §3).
package gh

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
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
}

// Snapshot is the tracked branches' pull requests, keyed by head branch.
type Snapshot struct {
	ByBranch map[string]PR `json:"by_branch"`
}

// bulkFields is the full read; gh pr list's own defaults (--state open --limit 30) would hide
// merged PRs and truncate below a busy repo's open count.
const bulkFields = "number,headRefName,headRefOid,baseRefName,baseRefOid,isDraft,state,statusCheckRollup,author"

// fallbackFields is the per-branch read. It is the only call that sees MERGED and CLOSED.
const fallbackFields = "number,state,baseRefName,headRefOid"

// List reads the pull requests for the tracked branches of the repo checked out at repoPath:
// one bulk read, then one fallback read per tracked branch the bulk read did not cover.
func List(ctx context.Context, repoPath string, tracked []string) (Snapshot, error) {
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

// Create opens a pull request for the branch checked out at repoPath against base, filling the
// title and body from the last commit and applying the keep-open label -- the whole of stacking
// on the GitHub side, and the label that defuses both repos' 14-day auto-close. body overrides
// --fill's body only for a stacked base's "Merge after #N" line (docs/prd-command-centre.md §
// Phase 4); empty for every root PR.
func Create(ctx context.Context, repoPath, base, body string) error {
	args := []string{"pr", "create", "--base", base, "--fill", "--label", "keep-open"}
	if body != "" {
		args = append(args, "--body", body)
	}
	_, err := run(ctx, repoPath, args...)
	return err
}

func run(ctx context.Context, repoPath string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = repoPath
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh %s in %s: %w: %s", args[0], repoPath, err, bytes.TrimSpace(stderr.Bytes()))
	}
	return out, nil
}

// rawPR mirrors gh's JSON exactly. Nothing outside this file may read these field names.
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
		})
	}
	return prs, nil
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

// normalise reduces the rollup to the latest completed run per check name. The rollup is a
// multiset of a union type: live data shows one name five times at one head SHA with mixed
// CANCELLED and SUCCESS, and StatusContext entries carrying no name at all.
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

// statusContextStatus maps a commit status's state onto a check run's status vocabulary, so
// everything downstream reads one shape.
func statusContextStatus(state string) string {
	switch state {
	case "SUCCESS", "FAILURE", "ERROR":
		return "COMPLETED"
	default:
		return "PENDING"
	}
}

// better prefers a completed run over an incomplete one, and the later start otherwise: a
// re-run in flight must not erase the last verdict the repo actually reached.
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
