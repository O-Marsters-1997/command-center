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

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

const defaultBaseBranch = "main"

// row's json tags are what GET /graph.json serves: the route marshals []Group verbatim, so this
// is the graph island's only view of the data too (docs/prds/prd-fleet-view.md § One derivation).
type Row struct {
	URL string `json:"url"`
	// Repo names the row's own configured repo, so a group kept whole by a member in scope can
	// still name where an out-of-scope sibling lives (CONTEXT.md § Scope, ADR 7).
	Repo string `json:"repo"`
	// Feature names the row's own tracker feature, so a group kept whole by a member in scope can
	// still name where an out-of-scope sibling lives (CONTEXT.md § Feature).
	Feature string `json:"feature"`
	// Title is empty when the tick's read did not cover the ticket: a fresh DB, or an issue past
	// `gh issue list`'s own 100-Row limit.
	Title      string `json:"title"`
	State      string `json:"state"`
	Reason     string `json:"reason"`
	Tone       string `json:"tone"`
	Unattended bool   `json:"unattended"`
	Alive      bool   `json:"alive"`
	// Verbs comes from internal/plan: which verbs a state offers is a decision, so it is table-
	// tested beside plan.Status rather than spelled out per state in the template.
	Verbs        []string `json:"verbs"`
	Branch       string   `json:"branch"`
	PendingVerbs []string `json:"pending_verbs"`
	Base         string   `json:"base"`
	// BaseVerdict is the base's own CI verdict label ("review_me"/"needs_you"/"checking"/
	// "base_moved"), empty for a root row: a red check on a descendant whose base moved may not
	// be its own fault (docs/designs/command-centre-design.md § 4a).
	BaseVerdict string `json:"base_verdict"`
	// StackDepth and MergeOrder are the row's distance from a root and the order it merges in,
	// bottom-up — the app never merges a PR, so this is the only place the order is shown
	// (docs/prds/prd-command-centre.md § The page → stack order).
	StackDepth int `json:"stack_depth"`
	MergeOrder int `json:"merge_order"`
	// Warning is invariant 2's hazard, named on the row: a non-main-based PR carrying
	// ready-to-merge would squash-merge into its parent branch with the parent's own checks
	// unseen, and empty otherwise. The app never applies that label itself.
	Warning  string   `json:"warning"`
	Blocking []string `json:"blocking"`
	Worktree string   `json:"worktree"`
	PRNumber int      `json:"pr_number"`
	PRState  string   `json:"pr_state"`
	// Pgid, Elapsed and LogPath are plain, copy-pasteable text (docs/prds/prd-command-centre.md §
	// The page) — empty for a ticket with no run yet.
	Pgid           string  `json:"pgid"`
	Elapsed        string  `json:"elapsed"`
	ElapsedSeconds int     `json:"elapsed_seconds"`
	ElapsedPercent int     `json:"elapsed_percent"`
	LogPath        string  `json:"log_path"`
	CancelCount    int     `json:"cancel_count"`
	SpendTokens    int     `json:"spend_tokens"`
	SpendUSD       float64 `json:"spend_usd"`
	SpendSettled   bool    `json:"spend_settled"`
	// BaselineSHA and Checks are the detail fragment's, not the board's: the row is derived once
	// and every island reads it (docs/prds/prd-operator-surface.md § One derivation).
	BaselineSHA string  `json:"baseline_sha"`
	Checks      []Check `json:"checks"`
	RedChecks   []Check `json:"red_checks"`
	// Draft mirrors the PR's own observed isDraft, not DraftGate's own opinion: a failed gh pr
	// ready leaves GitHub's real state unchanged, and this must still render honestly.
	Draft       bool   `json:"draft"`
	DraftReason string `json:"draft_reason"`
	// FirstPushCIFailed and HandChurnLines are nil-safe reads of the ticket's own columns:
	// false/0 render nothing, distinct from the column never having been recorded.
	FirstPushCIFailed bool    `json:"first_push_ci_failed"`
	HandChurnLines    int     `json:"hand_churn_lines"`
	AgentPctWeek      float64 `json:"agent_pct_week"`
	ResolvePctWeek    float64 `json:"resolve_pct_week"`
	FollowUpPctWeek   float64 `json:"follow_up_pct_week"`
	SpendPctWeek      float64 `json:"spend_pct_week"`
	TicketOpen        bool    `json:"ticket_open"`

	// Selected is this render's ?sel= row: the only one whose detail <tr> exists at all, so an
	// unattached hx-preserve id never lingers past the row that grew it
	// (docs/prds/prd-fleet-view.md § The hx-preserve id is conditional on selection).
	Selected bool `json:"selected"`
	Checked  bool `json:"checked"`
	// SelectPath/SelectPush toggle Selected; TogglePath/TogglePush toggle Checked. Each pair is
	// the board fragment to hx-get and the root path to hx-push-url.
	SelectPath string `json:"select_path"`
	SelectPush string `json:"select_push"`
	TogglePath string `json:"toggle_path"`
	TogglePush string `json:"toggle_push"`
	// VerbPath is /verb carrying this render's view state, so the swap handleVerb answers with
	// rebuilds the board the operator was looking at rather than the default one.
	VerbPath string `json:"verb_path"`
	// Log is the parsed run log, set only when Selected (docs/prds/prd-fleet-view.md § The run log).
	Log LogDetail `json:"log"`
	// RunID is the ticket's latest run, used to look up its context curve once selected. Zero for
	// a ticket with no run yet.
	RunID int64 `json:"-"`
	// ContextCurve is the selected run's per-request context chart, set only when Selected and the
	// run has recorded rows.
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

// FollowUpAvailable reports whether this row's state offers follow-up.
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

// DetailID is the row's stable DOM id. A ticket URL is neither a usable id nor a CSS selector,
// and htmx needs both: hx-target resolves the selector, and hx-preserve matches the id to keep
// an expanded detail mounted through the board's own five-second swap.
func (r Row) DetailID() string {
	sum := sha256.Sum256([]byte(r.URL))
	return "detail-" + hex.EncodeToString(sum[:6])
}

// Group is one blocker and the rows waiting on it. A row with no blocker in the ticket set is its
// own group with a nil Root (docs/prds/prd-operator-surface.md § Reading the board).
type Group struct {
	Root     *Row  `json:"root"`
	Children []Row `json:"children"`
}

// applyViewState parses the selected row's own log only, not every row's: a board of twenty-five
// rows must not open twenty-five log files to render one poll.
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

// deriveRows labels every row from the snapshot Derive made of the stored facts and this tick's
// observation (docs/designs/command-centre-design.md § Schema, inv. 14).
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
		verdictLabelByBranch[t.Branch] = plan.VerdictLabel(e.Run)
		baseByBranch[t.Branch] = e.Unlock.BaseBranch
		pr := in.Obs.PRs[plan.BranchKey(t.Repo, t.Branch)]
		var redLeaves []string
		if e.Run != nil {
			redLeaves = e.Run.RedLeaves
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
			URL:               t.URL,
			Repo:              t.Repo,
			Feature:           t.Feature,
			Title:             in.Obs.Titles[t.URL],
			State:             e.State.String(),
			Reason:            string(e.Reason),
			Tone:              plan.Tone(e.State),
			Unattended:        e.State.Unattended(),
			Alive:             in.Obs.Runs[t.URL].Alive,
			Verbs:             plan.Verbs(e.State),
			PendingVerbs:      in.PendingVerbs[t.URL],
			Branch:            t.Branch,
			Base:              e.Unlock.BaseBranch,
			Worktree:          in.Obs.Worktrees[plan.BranchKey(t.Repo, t.Branch)],
			PRNumber:          pr.Number,
			PRState:           pr.State.String(),
			Pgid:              pgid,
			Elapsed:           elapsed,
			ElapsedSeconds:    elapsedSeconds,
			LogPath:           e.LogPath,
			CancelCount:       membership.Members,
			Warning:           cmp.Or(readyToMergeWarning(pr), removalWarning(e.State, in.Removals[t.URL])),
			BaselineSHA:       latestRun.BaselineSHA,
			Checks:            sortedChecks(pr.Checks),
			RedChecks:         redChecksFor(redLeaves, pr.Checks),
			Blocking:          e.Unlock.Blocking,
			Draft:             pr.IsDraft,
			DraftReason:       e.DraftReason,
			FirstPushCIFailed: t.FirstPushCI != nil && !*t.FirstPushCI,
			HandChurnLines:    handChurnLines(t),
			RunID:             latestRun.ID,
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

// groupRows keys a fan-in row's group on the first blocker in its own Blocking
// (internal/plan/plan.go:119); Reason still names every blocker.
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

// filterGroupsByRepo narrows groups to a repo scope, admitting a matching group whole: keeping a
// group's root or any child out of it would leave groupRows' unchecked byURL[root] lookup
// pointing at nothing (ADR 7 "a scope admits a group whole"). An empty repo is unscoped.
func filterGroupsByRepo(groups []Group, repo string) []Group {
	return filterGroups(groups, repo, func(r Row) string { return r.Repo })
}

// filterGroupsByFeature narrows groups to a feature scope, following filterGroupsByRepo: it
// admits a matching group whole, and an empty feature is unscoped. Composed with
// filterGroupsByRepo in render, so both axes narrow independently rather than one overriding the
// other.
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

// rowsIn flattens groups back to the rows they render, which is what deriveBand counts: a scoped
// Group still shows its out-of-scope members, so the band counts them too (ADR 7).
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

// repoLinksFor is the breadcrumb's repo switcher: "all" plus one entry per configured repo, nil
// when none are configured so a single-repo fixture's breadcrumb renders no switcher at all.
func repoLinksFor(repos []config.Repo, params Params) []ScopeLink {
	if len(repos) == 0 {
		return nil
	}
	links := make([]ScopeLink, 0, len(repos)+1)
	links = append(links, ScopeLink{Name: "all", Path: params.withRepo("").pagePath(), Current: params.Repo == ""})
	for _, r := range repos {
		links = append(links,
			ScopeLink{Name: r.Name, Path: params.withRepo(r.Name).pagePath(), Current: params.Repo == r.Name})
	}
	return links
}

// distinctFeatures is the sorted set of non-blank Feature values among tickets, which render also
// uses to blank an unrecognised ?feature=.
func distinctFeatures(tickets []store.Ticket) []string {
	seen := make(map[string]bool)
	for _, t := range tickets {
		if t.Feature != "" {
			seen[t.Feature] = true
		}
	}
	return slices.Sorted(maps.Keys(seen))
}

// readyToMergeWarning names invariant 2's hazard for the page: pr.BaseRef is what GitHub
// actually has the PR targeting, which is what matters here, not the base this app would itself
// choose (docs/designs/command-centre-design.md § 4a inv. 2).
func readyToMergeWarning(pr plan.PR) string {
	if !plan.StackedReadyToMergeWarning(pr.BaseRef, pr.Labels) {
		return ""
	}
	return fmt.Sprintf(
		"ready-to-merge on a non-main base (%s): would squash into the parent branch, checks unseen", pr.BaseRef)
}

// removalWarning surfaces the row's own remove-worktree refusal, only while its state still
// offers the verb -- once the row leaves that state (removed, or no longer eligible), a refusal
// from before must never render as if it were today's (docs/adr/0008-cc-proves-what-tp-cannot.md).
func removalWarning(s plan.State, detail string) string {
	if detail == "" || !slices.Contains(plan.Verbs(s), plan.VerbRemoveWorktree) {
		return ""
	}
	return "worktree removal refused: " + detail
}

func handChurnLines(t store.Ticket) int {
	if t.HandChurnLines == nil {
		return 0
	}
	return *t.HandChurnLines
}
