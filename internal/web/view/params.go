package view

import (
	"net/url"
	"slices"
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
	Log     string
	Repo    string
	Feature string
	Filter  string
	All     bool
}

func ParseParams(q url.Values) Params {
	v := Params{
		Tickets: q["ticket"], Log: NormalizeLogFilter(q.Get("log")),
		Repo: q.Get("repo"), Feature: q.Get("feature"),
		Filter: normalizeFilter(q.Get("filter")), All: q.Get("all") == "1",
	}
	if sel := q["sel"]; len(sel) > 0 {
		v.Sel = sel[0]
	}
	return v
}

func normalizeFilter(filter string) string {
	for _, s := range sectionOrder {
		if s.Key == filter {
			return filter
		}
	}
	return ""
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

func (v Params) query() string {
	q := url.Values{}
	if v.All {
		q.Set("all", "1")
	}
	if v.Filter != "" {
		q.Set("filter", v.Filter)
	}
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
	return q.Encode()
}

func (v Params) boardPath() string { return withQuery("/board", v.query()) }
func (v Params) verbPath() string  { return withQuery("/verb", v.query()) }

func (v Params) pagePath() string {
	rest := v
	switch {
	case v.All:
		rest.All = false
		return withQuery(ticketsPath, rest.query())
	case v.Repo != "":
		rest.Repo = ""
		return withQuery(RepoPath(v.Repo), rest.query())
	case v.Feature != "":
		rest.Feature = ""
		return withQuery("/f/"+url.PathEscape(v.Feature), rest.query())
	default:
		return withQuery(ticketsPath, v.query())
	}
}

func withQuery(path, query string) string {
	if query == "" {
		return path
	}
	return path + "?" + query
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
