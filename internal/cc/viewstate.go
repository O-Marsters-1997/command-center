package cc

import (
	"net/url"
	"slices"
)

// logFilters names the four ?log= modes the run log accepts, in the order they render as
// buttons. "all" is the default and is never written into a URL.
var logFilters = []string{"all", "skills", "tools", "fails"}

// normalizeLogFilter rejects anything but the four named modes, onto "all" — a query string is
// user input, and an unrecognised mode is silently the default rather than an error.
func normalizeLogFilter(mode string) string {
	if slices.Contains(logFilters, mode) {
		return mode
	}
	return "all"
}

type viewParams struct {
	// Sel is the one expanded row. At most one: parseViewParams takes the first ?sel= and drops
	// the rest.
	Sel     string
	Tickets []string
	View    string
	// Log is the selected row's run-log filter (docs/prds/prd-fleet-view.md § The run log).
	Log string
	// Repo is the board's repo scope, raw off the query string. render blanks it against the
	// configured repo names, since that list is not available at parse time (CONTEXT.md § Scope).
	Repo string
	// Feature is the board's feature scope, raw off the query string. render blanks it against the
	// distinct features among the loaded tickets, since that set is not available at parse time
	// (CONTEXT.md § Feature).
	Feature string
}

func parseViewParams(q url.Values) viewParams {
	v := viewParams{
		Tickets: q["ticket"], View: q.Get("view"), Log: normalizeLogFilter(q.Get("log")),
		Repo: q.Get("repo"), Feature: q.Get("feature"),
	}
	if sel := q["sel"]; len(sel) > 0 {
		v.Sel = sel[0]
	}
	if v.View == "" {
		v.View = "board"
	}
	return v
}

// normalizeRepoScope blanks a ?repo= value unrecognised against the configured repos, following
// normalizeLogFilter: a query string is user input, and an unknown scope shows the unscoped board
// rather than an error. configuredRepos is keyed by repo name; the caller passes stackingByRepo
// since it is already indexed that way, though this reads it purely as a membership set.
func normalizeRepoScope(repo string, configuredRepos map[string]bool) string {
	if _, ok := configuredRepos[repo]; ok {
		return repo
	}
	return ""
}

// normalizeFeatureScope blanks a ?feature= value unrecognised against the features currently in
// the fleet, following normalizeLogFilter -- features are the tracker's own and unconfigured, so
// the membership set is the distinct Feature values render already read off store.Tickets.
func normalizeFeatureScope(feature string, fleetFeatures []string) string {
	if slices.Contains(fleetFeatures, feature) {
		return feature
	}
	return ""
}

// url.Values.Encode sorts by key, so this always renders feature/log/repo/sel/ticket/view in that
// order.
func (v viewParams) query() string {
	q := url.Values{}
	if v.Feature != "" {
		q.Set("feature", v.Feature)
	}
	if v.Log != "" && v.Log != "all" {
		q.Set("log", v.Log)
	}
	if v.Repo != "" {
		q.Set("repo", v.Repo)
	}
	if v.Sel != "" {
		q.Set("sel", v.Sel)
	}
	for _, ticket := range v.Tickets {
		q.Add("ticket", ticket)
	}
	if v.View != "" && v.View != "board" {
		q.Set("view", v.View)
	}
	return q.Encode()
}

func (v viewParams) withLog(mode string) viewParams {
	next := v
	next.Log = mode
	return next
}

func (v viewParams) withRepo(repo string) viewParams {
	next := v
	next.Repo = repo
	return next
}

func (v viewParams) withFeature(feature string) viewParams {
	next := v
	next.Feature = feature
	return next
}

func (v viewParams) boardPath() string { return withQuery("/board", v.query()) }
func (v viewParams) verbPath() string  { return withQuery("/verb", v.query()) }
func (v viewParams) pagePath() string  { return withQuery("/", v.query()) }

func withQuery(path, query string) string {
	if query == "" {
		return path
	}
	return path + "?" + query
}

func (v viewParams) toggleSel(ticketURL string) viewParams {
	next := v
	if v.Sel == ticketURL {
		next.Sel = ""
	} else {
		next.Sel = ticketURL
	}
	return next
}

// toggleTicket never mutates v.Tickets's own backing array: v is still rendered after this call.
func (v viewParams) toggleTicket(ticketURL string) viewParams {
	next := v
	if slices.Contains(v.Tickets, ticketURL) {
		next.Tickets = slices.DeleteFunc(slices.Clone(v.Tickets), func(t string) bool { return t == ticketURL })
	} else {
		next.Tickets = append(slices.Clone(v.Tickets), ticketURL)
	}
	return next
}
