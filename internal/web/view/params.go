package view

import (
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/store"
)

var logFilters = []string{"all", "skills", "tools", "fails"}

// NormalizeLogFilter maps anything but the four named modes onto "all".
func NormalizeLogFilter(mode string) string {
	if slices.Contains(logFilters, mode) {
		return mode
	}
	return "all"
}

type Params struct {
	Sel     string
	Tickets []string
	View    string
	Log     string
	Repo    string
	Feature string
	Phase   string
}

func ParseParams(q url.Values) Params {
	v := Params{
		Tickets: q["ticket"], View: q.Get("view"), Log: NormalizeLogFilter(q.Get("log")),
		Repo: q.Get("repo"), Feature: q.Get("feature"), Phase: normalizePhase(q.Get("phase")),
	}
	if sel := q["sel"]; len(sel) > 0 {
		v.Sel = sel[0]
	}
	if v.View == "" {
		v.View = "board"
	}
	return v
}

func normalizePhase(phase string) string {
	if i, err := strconv.Atoi(phase); err != nil || i < 0 {
		return ""
	}
	return phase
}

func normalizeRepoScope(repo string, tracked []store.Repo) string {
	i := slices.IndexFunc(tracked, func(r store.Repo) bool { return strings.EqualFold(r.Name, repo) })
	if i < 0 || repo == "" {
		return ""
	}
	return tracked[i].Name
}

func normalizeFeatureScope(feature string, fleetFeatures []string) string {
	if slices.Contains(fleetFeatures, feature) {
		return feature
	}
	return ""
}

// url.Values.Encode sorts by key, so this always renders feature/log/phase/repo/sel/ticket/view
// in that order.
func (v Params) query() string {
	q := url.Values{}
	if v.Feature != "" {
		q.Set("feature", v.Feature)
	}
	if v.Log != "" && v.Log != "all" {
		q.Set("log", v.Log)
	}
	if v.Phase != "" {
		q.Set("phase", v.Phase)
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

func (v Params) withLog(mode string) Params {
	next := v
	next.Log = mode
	return next
}

func (v Params) withPhase(phase string) Params {
	next := v
	next.Phase = phase
	return next
}

func (v Params) withRepo(repo string) Params {
	next := v
	next.Repo = repo
	return next
}

func (v Params) withFeature(feature string) Params {
	next := v
	next.Feature = feature
	return next
}

func (v Params) boardPath() string { return withQuery("/board", v.query()) }
func (v Params) verbPath() string  { return withQuery("/verb", v.query()) }

// pagePath is the URL a board swap pushes: a feature-scoped board lives at /f/{feature}.
func (v Params) pagePath() string {
	if v.Feature == "" || v.Repo != "" || (v.View != "" && v.View != "board") {
		return withQuery("/", v.query())
	}
	rest := v
	rest.Feature = ""
	return withQuery("/f/"+url.PathEscape(v.Feature), rest.query())
}

func (v Params) featureImportPath() string {
	return withQuery("/features/"+url.PathEscape(v.Feature)+"/import", v.query())
}

func withQuery(path, query string) string {
	if query == "" {
		return path
	}
	return path + "?" + query
}

func (v Params) toggleSel(ticketURL string) Params {
	next := v
	next.Phase = ""
	if v.Sel == ticketURL {
		next.Sel = ""
	} else {
		next.Sel = ticketURL
	}
	return next
}

func (v Params) toggleTicket(ticketURL string) Params {
	next := v
	if slices.Contains(v.Tickets, ticketURL) {
		next.Tickets = slices.DeleteFunc(slices.Clone(v.Tickets), func(t string) bool { return t == ticketURL })
	} else {
		next.Tickets = append(slices.Clone(v.Tickets), ticketURL)
	}
	return next
}
