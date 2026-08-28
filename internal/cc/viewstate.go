package cc

import (
	"net/url"
	"slices"
)

type viewParams struct {
	// Sel is the one expanded row. At most one: parseViewParams takes the first ?sel= and drops
	// the rest.
	Sel   string
	Tasks []string
	View  string
}

func parseViewParams(q url.Values) viewParams {
	v := viewParams{Tasks: q["task"], View: q.Get("view")}
	if sel := q["sel"]; len(sel) > 0 {
		v.Sel = sel[0]
	}
	if v.View == "" {
		v.View = "board"
	}
	return v
}

// url.Values.Encode sorts by key, so this always renders sel/task/view in that order.
func (v viewParams) query() string {
	q := url.Values{}
	if v.Sel != "" {
		q.Set("sel", v.Sel)
	}
	for _, task := range v.Tasks {
		q.Add("task", task)
	}
	if v.View != "" && v.View != "board" {
		q.Set("view", v.View)
	}
	return q.Encode()
}

func (v viewParams) boardPath() string { return withQuery("/board", v.query()) }
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

// toggleTask never mutates v.Tasks's own backing array: v is still rendered after this call.
func (v viewParams) toggleTask(ticketURL string) viewParams {
	next := v
	if slices.Contains(v.Tasks, ticketURL) {
		next.Tasks = slices.DeleteFunc(slices.Clone(v.Tasks), func(t string) bool { return t == ticketURL })
	} else {
		next.Tasks = append(slices.Clone(v.Tasks), ticketURL)
	}
	return next
}
