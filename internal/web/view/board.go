package view

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"path"
	"slices"
	"strconv"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

const defaultBaseBranch = "main"

type Row struct {
	URL     string `json:"url"`
	Repo    string `json:"repo"`
	Feature string `json:"feature"`
	// Title is empty when the tick's read did not cover the ticket: a fresh DB, or an issue past
	// `gh issue list`'s own 100-row limit.
	Title           string   `json:"title"`
	State           string   `json:"state"`
	Reason          string   `json:"reason"`
	Tone            string   `json:"tone"`
	Unattended      bool     `json:"unattended"`
	Alive           bool     `json:"alive"`
	Verbs           []string `json:"verbs"`
	Branch          string   `json:"branch"`
	BlockedBy       []string `json:"blocked_by"`
	PendingVerbs    []string `json:"pending_verbs"`
	Base            string   `json:"base"`
	BaseVerdict     string   `json:"base_verdict"`
	StackDepth      int      `json:"stack_depth"`
	MergeOrder      int      `json:"merge_order"`
	Warning         string   `json:"warning"`
	Blocking        []string `json:"blocking"`
	Worktree        string   `json:"worktree"`
	PRNumber        int      `json:"pr_number"`
	PRState         string   `json:"pr_state"`
	Pgid            string   `json:"pgid"`
	Elapsed         string   `json:"elapsed"`
	ElapsedSeconds  int      `json:"elapsed_seconds"`
	ElapsedPercent  int      `json:"elapsed_percent"`
	LogPath         string   `json:"log_path"`
	CancelCount     int      `json:"cancel_count"`
	SpendTokens     int      `json:"spend_tokens"`
	SpendUSD        float64  `json:"spend_usd"`
	SpendSettled    bool     `json:"spend_settled"`
	BaselineSHA     string   `json:"baseline_sha"`
	Checks          []Check  `json:"checks"`
	RedChecks       []Check  `json:"red_checks"`
	Draft           bool     `json:"draft"`
	DraftReason     string   `json:"draft_reason"`
	AgentPctWeek    float64  `json:"agent_pct_week"`
	ResolvePctWeek  float64  `json:"resolve_pct_week"`
	FollowUpPctWeek float64  `json:"follow_up_pct_week"`
	SpendPctWeek    float64  `json:"spend_pct_week"`
	TicketOpen      bool     `json:"ticket_open"`

	// Selected is the ?sel= row, the only one with a detail <tr>, so an unattached hx-preserve id
	// never lingers past the row that grew it.
	Selected     bool         `json:"selected"`
	Checked      bool         `json:"checked"`
	SelectPath   string       `json:"select_path"`
	SelectPush   string       `json:"select_push"`
	TogglePath   string       `json:"toggle_path"`
	TogglePush   string       `json:"toggle_push"`
	VerbPath     string       `json:"verb_path"`
	Log          LogDetail    `json:"log"`
	RunID        int64        `json:"-"`
	ContextCurve ContextCurve `json:"-"`
}

type Check struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	DetailsURL string `json:"details_url"`
}

func (r Row) Ticket() string { return ticketRef(r.URL) }

func ticketRef(url string) string { return "#" + path.Base(url) }

func (r Row) FollowUpAvailable() bool { return slices.Contains(r.Verbs, plan.VerbFollowUp) }

func (r Row) Stack() string {
	if r.Base == "" || r.Base == defaultBaseBranch {
		return fmt.Sprintf("L%d", r.MergeOrder)
	}
	return fmt.Sprintf("L%d ← %s", r.MergeOrder, r.Base)
}

func (r Row) ChecksPassed() int {
	passed := 0
	for _, c := range r.Checks {
		if c.Conclusion == "SUCCESS" {
			passed++
		}
	}
	return passed
}

// DetailID is the row's DOM id: hx-target needs a selector and hx-preserve an id, and a ticket
// URL is neither.
func (r Row) DetailID() string {
	sum := sha256.Sum256([]byte(r.URL))
	return "detail-" + hex.EncodeToString(sum[:6])
}

// Group is one blocker and the rows waiting on it. A row with no blocker is its own group with a
// nil Root.
type Group struct {
	Root     *Row  `json:"root"`
	Children []Row `json:"children"`
}

func applyViewState(rows []Row, params Params, render LineRenderer) {
	for i := range rows {
		r := &rows[i]
		r.Selected = params.Sel == r.URL
		r.Checked = slices.Contains(params.Tickets, r.URL)
		toggledSel := params.toggleSel(r.URL)
		r.SelectPath, r.SelectPush = toggledSel.boardPath(), toggledSel.pagePath()
		toggledTicket := params.toggleTicket(r.URL)
		r.TogglePath, r.TogglePush = toggledTicket.boardPath(), toggledTicket.pagePath()
		r.VerbPath = params.verbPath()
		if r.Selected {
			r.Log = buildLogDetail(render, r.LogPath, r.Alive, r.URL, params)
		}
	}
}

func deriveRows(tickets []store.Ticket, in plan.Input, snap plan.Snapshot) []Row {
	rows := make([]Row, 0, len(tickets))
	verdictLabelByBranch := make(map[string]string, len(tickets))
	baseByBranch := make(map[string]string, len(tickets))
	for _, t := range tickets {
		e, ok := snap.Entry(t.URL)
		if !ok {
			continue
		}
		membership := in.Memberships[t.URL]
		latestRun := in.Runs[t.URL]
		verdictLabelByBranch[t.Branch] = runVerdictLabel(e.Run)
		baseByBranch[t.Branch] = e.Unlock.BaseBranch
		pr := in.Obs.PRs[plan.BranchKey(t.Repo, t.Branch)]
		var redLeaves []string
		if e.Run != nil && e.Run.Verdict != nil {
			redLeaves = e.Run.Verdict.RedLeaves
		}
		var pgid, elapsed string
		var elapsedSeconds int
		if e.Pgid != nil {
			pgid = strconv.Itoa(*e.Pgid)
		}
		if e.Elapsed != nil {
			elapsed = e.Elapsed.String()
			elapsedSeconds = int(e.Elapsed.Seconds())
		}
		rows = append(rows, Row{
			URL:            t.URL,
			Repo:           t.Repo,
			Feature:        t.Feature,
			Title:          in.Obs.Titles[t.URL],
			State:          e.State.String(),
			Reason:         string(e.Reason),
			Tone:           plan.Tone(e.State),
			Unattended:     e.State.Unattended(),
			Alive:          in.Obs.Runs[t.URL].Alive,
			Verbs:          plan.Verbs(e.State),
			PendingVerbs:   in.PendingVerbs[t.URL],
			Branch:         t.Branch,
			BlockedBy:      t.BlockedBy,
			Base:           e.Unlock.BaseBranch,
			Worktree:       in.Obs.Worktrees[plan.BranchKey(t.Repo, t.Branch)],
			PRNumber:       pr.Number,
			PRState:        pr.State.String(),
			Pgid:           pgid,
			Elapsed:        elapsed,
			ElapsedSeconds: elapsedSeconds,
			LogPath:        e.LogPath,
			CancelCount:    membership.Members,
			Warning:        cmp.Or(readyToMergeWarning(pr), removalWarning(e.State, in.Removals[t.URL])),
			BaselineSHA:    latestRun.BaselineSHA,
			Checks:         sortedChecks(pr.Checks),
			RedChecks:      redChecksFor(redLeaves, pr.Checks),
			Blocking:       e.Unlock.Blocking,
			Draft:          pr.IsDraft,
			DraftReason:    e.DraftReason,
			RunID:          latestRun.ID,
		})
	}

	longestElapsed := 0
	for _, r := range rows {
		longestElapsed = max(longestElapsed, r.ElapsedSeconds)
	}
	for i := range rows {
		if rows[i].Base != "" && rows[i].Base != defaultBaseBranch {
			rows[i].BaseVerdict = verdictLabelByBranch[rows[i].Base]
		}
		rows[i].StackDepth = plan.StackDepth(rows[i].Branch, baseByBranch)
		rows[i].MergeOrder = rows[i].StackDepth + 1
		rows[i].ElapsedPercent = PercentOf(rows[i].ElapsedSeconds, longestElapsed)
	}
	return rows
}

func PercentOf(part, total int) int {
	if total <= 0 {
		return 0
	}
	return part * 100 / total
}

func sortedChecks(checks map[string]plan.CheckState) []Check {
	out := make([]Check, 0, len(checks))
	for _, name := range slices.Sorted(maps.Keys(checks)) {
		cs := checks[name]
		out = append(out, Check{Name: name, Status: cs.Status, Conclusion: cs.Conclusion, DetailsURL: cs.DetailsURL})
	}
	return out
}

func redChecksFor(redLeaves []string, checks map[string]plan.CheckState) []Check {
	if len(redLeaves) == 0 {
		return nil
	}
	red := make(map[string]bool, len(redLeaves))
	for _, name := range redLeaves {
		red[name] = true
	}
	out := make([]Check, 0, len(redLeaves))
	for _, c := range sortedChecks(checks) {
		if red[c.Name] {
			out = append(out, c)
		}
	}
	return out
}

func groupRows(rows []Row) []Group {
	byURL := make(map[string]Row, len(rows))
	childrenByRoot := make(map[string][]Row, len(rows))
	for _, r := range rows {
		byURL[r.URL] = r
		if len(r.Blocking) > 0 {
			root := r.Blocking[0]
			childrenByRoot[root] = append(childrenByRoot[root], r)
		}
	}

	isChild := make(map[string]bool, len(rows))
	for _, children := range childrenByRoot {
		for _, c := range children {
			isChild[c.URL] = true
		}
	}

	rootURLs := make([]string, 0, len(childrenByRoot))
	for root := range childrenByRoot {
		if !isChild[root] {
			rootURLs = append(rootURLs, root)
		}
	}
	slices.Sort(rootURLs)

	groups := make([]Group, 0, len(rootURLs)+len(rows))
	for _, root := range rootURLs {
		children := flattenChain(root, childrenByRoot)
		slices.SortFunc(children, func(a, b Row) int {
			if a.MergeOrder != b.MergeOrder {
				return cmp.Compare(a.MergeOrder, b.MergeOrder)
			}
			return cmp.Compare(a.URL, b.URL)
		})
		rootRow := byURL[root]
		groups = append(groups, Group{Root: &rootRow, Children: children})
	}

	var ungrouped []Row
	for _, r := range rows {
		if len(r.Blocking) == 0 {
			if _, isRoot := childrenByRoot[r.URL]; !isRoot {
				ungrouped = append(ungrouped, r)
			}
		}
	}
	slices.SortFunc(ungrouped, func(a, b Row) int { return cmp.Compare(a.URL, b.URL) })
	for _, r := range ungrouped {
		groups = append(groups, Group{Children: []Row{r}})
	}
	return groups
}

func flattenChain(root string, childrenByRoot map[string][]Row) []Row {
	var out []Row
	seen := map[string]bool{root: true}
	queue := []string{root}
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		for _, c := range childrenByRoot[u] {
			if seen[c.URL] {
				continue
			}
			seen[c.URL] = true
			out = append(out, c)
			queue = append(queue, c.URL)
		}
	}
	return out
}

func filterGroupsByRepo(groups []Group, repo string) []Group {
	return filterGroups(groups, repo, func(r Row) string { return r.Repo })
}

func filterGroupsByFeature(groups []Group, feature string) []Group {
	return filterGroups(groups, feature, func(r Row) string { return r.Feature })
}

func filterGroups(groups []Group, scope string, scopeOf func(Row) string) []Group {
	if scope == "" {
		return groups
	}
	filtered := make([]Group, 0, len(groups))
	for _, g := range groups {
		if groupInScope(g, scope, scopeOf) {
			filtered = append(filtered, g)
		}
	}
	return filtered
}

func groupInScope(g Group, scope string, scopeOf func(Row) string) bool {
	if g.Root != nil && scopeOf(*g.Root) == scope {
		return true
	}
	return slices.ContainsFunc(g.Children, func(c Row) bool { return scopeOf(c) == scope })
}

func rowsIn(groups []Group) []Row {
	rows := make([]Row, 0, len(groups))
	for _, g := range groups {
		if g.Root != nil {
			rows = append(rows, *g.Root)
		}
		rows = append(rows, g.Children...)
	}
	return rows
}

func distinctFeatures(tickets []store.Ticket) []string {
	seen := make(map[string]bool)
	for _, t := range tickets {
		if t.Feature != "" {
			seen[t.Feature] = true
		}
	}
	return slices.Sorted(maps.Keys(seen))
}

func readyToMergeWarning(pr plan.PR) string {
	if !plan.StackedReadyToMergeWarning(pr.BaseRef, pr.Labels) {
		return ""
	}
	return fmt.Sprintf(
		"ready-to-merge on a non-main base (%s): would squash into the parent branch, checks unseen", pr.BaseRef)
}

func removalWarning(s plan.State, detail string) string {
	if detail == "" || !slices.Contains(plan.Verbs(s), plan.VerbRemoveWorktree) {
		return ""
	}
	return "worktree removal refused: " + detail
}

func runVerdictLabel(run *plan.RunFact) string {
	if run == nil {
		return ""
	}
	return run.Verdict.Label()
}
