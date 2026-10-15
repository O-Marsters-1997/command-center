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

	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

const defaultBaseBranch = "main"

type Row struct {
	URL        string
	Repo       string
	Feature    string
	Project    string
	ProjectHue int
	// GitHub's issue list caps reads at 100 rows, leaving titles beyond the limit empty.
	Title           string
	State           string
	Reason          string
	Question        string
	Glyph           plan.GlyphWord
	Alive           bool
	Verbs           []plan.Verb
	Branch          string
	BlockedBy       []string
	PendingVerbs    []plan.Verb
	Base            string
	BaseVerdict     string
	StackDepth      int
	MergeOrder      int
	Warning         string
	Blocking        []string
	After           []string
	Unlocks         []string
	Worktree        string
	PRNumber        int
	PRState         string
	Pgid            string
	Elapsed         string
	ElapsedSeconds  int
	ElapsedPercent  int
	LogPath         string
	CancelCount     int
	SpendTokens     int
	Checks          []Check
	RedChecks       []Check
	Draft           bool
	DraftReason     string
	AgentPctWeek    float64
	FollowUpPctWeek float64
	SpendPctWeek    float64
	TicketOpen      bool

	Checked     bool
	Launchable  bool
	Buttons     []VerbButton
	SessionPath string
	TogglePath  string
	TogglePush  string
	VerbPath    string
	Log         LogDetail
}

type Check struct {
	Name       string
	Status     string
	Conclusion string
	DetailsURL string
}

func (r Row) Ticket() string { return ticketRef(r.URL) }

func ticketRef(url string) string { return "#" + path.Base(url) }

func (r Row) FollowUpAvailable() bool { return slices.Contains(r.Verbs, plan.VerbFollowUp) }

func (r Row) AwaitingInput() bool { return r.State == plan.AwaitingInput.String() }

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

// DetailID supplies the selector and id required by htmx's hx-target and hx-preserve.
func (r Row) DetailID() string {
	sum := sha256.Sum256([]byte(r.URL))
	return "detail-" + hex.EncodeToString(sum[:6])
}

type Group struct {
	Root     option.Option[Row]
	Children []Row
}

func applyViewState(rows []Row, params Params, records []store.Event, render LineRenderer) error {
	for i := range rows {
		r := &rows[i]
		r.Checked = slices.Contains(params.Tickets, r.URL)
		r.SessionPath = params.withScope(cmp.Or(SessionPath(r.URL), ticketsPath))
		toggledTicket := params.toggleTicket(r.URL)
		r.TogglePath, r.TogglePush = toggledTicket.boardPath(), toggledTicket.pagePath()
		r.VerbPath = params.verbPath()
		r.Launchable = slices.Contains(r.Verbs, plan.VerbLaunch)
		r.Buttons = r.VerbButtons()
		if params.Sel == r.URL {
			src := logSource{Path: r.LogPath, Streaming: r.Alive, TicketURL: r.URL, Records: records}
			log, err := buildLogDetail(render, src, params)
			if err != nil {
				return fmt.Errorf("build log for %s: %w", r.URL, err)
			}
			r.Log = log
		}
	}
	return nil
}

func deriveRows(tickets []store.Ticket, in plan.Input, snap plan.Snapshot, projects map[string]ProjectMark) []Row {
	rows := make([]Row, 0, len(tickets))
	verdictLabelByBranch := make(map[string]string, len(tickets))
	baseByBranch := make(map[string]string, len(tickets))
	for _, t := range tickets {
		url := plan.TicketURL(t.URL)
		e, ok := snap.Entry(url)
		if !ok {
			continue
		}
		membership := in.Memberships[url]
		latestRun := in.Runs[url]
		verdictLabelByBranch[t.Branch] = runVerdictLabel(e.Run)
		baseByBranch[t.Branch] = e.Unlock.BaseBranch
		pr := in.Obs.PRs[t.Ref()]
		var redLeaves []string
		if e.Run.IsSome() && e.Run.Unwrap().Verdict.IsSome() {
			redLeaves = e.Run.Unwrap().Verdict.Unwrap().RedLeaves
		}
		question := ""
		if e.State == plan.AwaitingInput {
			question = latestRun.Question
		}
		var pgid, elapsed string
		var elapsedSeconds int
		if e.Pgid.IsSome() {
			pgid = strconv.Itoa(e.Pgid.Unwrap())
		}
		if e.Elapsed.IsSome() {
			elapsed = e.Elapsed.Unwrap().String()
			elapsedSeconds = int(e.Elapsed.Unwrap().Seconds())
		}
		rows = append(rows, Row{
			URL:            t.URL,
			Repo:           t.Repo,
			Feature:        t.Feature,
			Project:        projects[t.Repo].Name,
			ProjectHue:     projects[t.Repo].Hue,
			Title:          in.Obs.Titles[url],
			State:          e.State.String(),
			Reason:         string(e.Reason),
			Question:       question,
			Glyph:          e.State.Glyph(),
			Alive:          in.Obs.Runs[url].Alive,
			Verbs:          plan.Verbs(e.State),
			PendingVerbs:   in.PendingVerbs[url],
			Branch:         t.Branch,
			BlockedBy:      t.BlockedBy,
			Base:           e.Unlock.BaseBranch,
			Worktree:       in.Obs.Worktrees[t.Ref()],
			PRNumber:       pr.Number,
			PRState:        pr.State.String(),
			Pgid:           pgid,
			Elapsed:        elapsed,
			ElapsedSeconds: elapsedSeconds,
			LogPath:        e.LogPath,
			CancelCount:    membership.Members,
			Warning:        cmp.Or(readyToMergeWarning(pr), removalWarning(e.State, in.Removals[url])),
			Checks:         sortedChecks(pr.Checks),
			RedChecks:      redChecksFor(redLeaves, pr.Checks),
			Blocking:       urlStrings(e.Unlock.Blocking),
			Draft:          pr.IsDraft,
			DraftReason:    e.DraftReason,
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
	nameDependencies(rows, in.Tickets)
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

func urlStrings(urls []plan.TicketURL) []string {
	out := make([]string, len(urls))
	for i, u := range urls {
		out[i] = string(u)
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
		groups = append(groups, Group{Root: option.Some(rootRow), Children: children})
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

func filterGroupsByProject(groups []Group, repos []string) []Group {
	inProject := func(r Row) bool { return slices.Contains(repos, r.Repo) }
	return slices.DeleteFunc(slices.Clone(groups), func(g Group) bool {
		return (g.Root.IsNone() || !inProject(g.Root.Unwrap())) && !slices.ContainsFunc(g.Children, inProject)
	})
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
	if g.Root.IsSome() && scopeOf(g.Root.Unwrap()) == scope {
		return true
	}
	return slices.ContainsFunc(g.Children, func(c Row) bool { return scopeOf(c) == scope })
}

func rowsIn(groups []Group) []Row {
	rows := make([]Row, 0, len(groups))
	for _, g := range groups {
		if g.Root.IsSome() {
			rows = append(rows, g.Root.Unwrap())
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

func runVerdictLabel(run option.Option[plan.RunFact]) string {
	if run.IsNone() || run.Unwrap().Verdict.IsNone() {
		return ""
	}
	return run.Unwrap().Verdict.Unwrap().Kind.String()
}
