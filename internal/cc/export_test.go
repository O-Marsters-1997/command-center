package cc

import (
	"context"
	"html/template"
	"net/http"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func RenderStatesPage(states []plan.State) (string, error) {
	return renderStates(page, states)
}

func RenderStatesBoard(states []plan.State) (string, error) {
	return renderStates(boardFragment, states)
}

func renderStates(tmpl *template.Template, states []plan.State) (string, error) {
	view := pageView{chrome: chrome{Observe: ageView{Age: "0s ago"}}}
	for _, state := range states {
		r := row{
			URL:        "sandbox://" + state.String(),
			State:      state.String(),
			Tone:       plan.Tone(state),
			Unattended: state.Unattended(),
			Verbs:      plan.Verbs(state),
		}
		view.Groups = append(view.Groups, group{Children: []row{r}})
	}

	var out strings.Builder
	if err := tmpl.Execute(&out, view); err != nil {
		return "", err
	}
	return out.String(), nil
}

// RenderLogLine exposes the one "logline" template both the detail render and the SSE stream
// render through, so a test can build its own expected markup rather than hand-copying it.
func RenderLogLine(e agentlog.Event, anchor bool) (template.HTML, error) {
	return renderLogLine(e, anchor)
}

// ReadTestdata and WriteRunLog let logstream_test.go and detail_test.go, both package cc_test,
// share the one fixture-reading and fixture-writing helper logview_test.go already defines
// rather than keeping a second copy under a different name.
func ReadTestdata(name string) string              { return mustReadTestdata(name) }
func WriteRunLog(t *testing.T, body string) string { return writeRunLog(t, body) }

// SameRemote is the git-URL comparison EnsureCheckout refuses on.
func SameRemote(a, b string) bool { return sameRemote(a, b) }

// MergifyHash is the observe phase's read of .mergify.yml off origin's default branch.
func MergifyHash(ctx context.Context, repoPath string) (string, error) {
	return mergifyHash(ctx, repoPath)
}

// BranchKey names one branch in every branch-keyed map on Observation, letting cc_test build
// fixtures against the same key production code reads.
func BranchKey(repo, branch string) string { return branchKey(repo, branch) }

// MainTipKey names defaultBaseBranch's own tip in Observation.BranchTips.
func MainTipKey(repo string) string { return mainTipKey(repo) }

// RegisterTestRoute adds a POST route directly to the server's own mux, letting a test prove that
// cross-origin protection covers a route it was never told to wrap, not just the five it replaced.
func (s *Server) RegisterTestRoute(pattern string, h http.HandlerFunc) {
	s.rawMux.HandleFunc(pattern, h)
}
