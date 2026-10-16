package web_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web/htmltest"
)

func TestHomePage(t *testing.T) {
	t.Parallel()

	failed := func(t *testing.T) *storepkg.Store {
		t.Helper()
		st, _ := sessionStoreEnding(t, sessionRunOpts{Kind: "agent", Outcome: plan.OutcomeFailed})
		return st
	}
	for _, tt := range []struct {
		name  string
		store func(*testing.T) *storepkg.Store
		want  string
	}{
		{name: "hops to the tickets page on a desktop", store: seededStoreAtTestNow, want: "/tickets"},
		{name: "hops to a session that needs the user", store: failed, want: "/s/acme/web/1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			main := htmltest.Parse(t, renderPath(t, newServer(tt.store(t), testNow), "/")).One(t, "main#main")
			if _, ok := main.Attr("data-home"); !ok {
				t.Errorf("GET / main lacks data-home")
			}
			if got, _ := main.Attr("data-desktop-target"); got != tt.want {
				t.Errorf("GET / data-desktop-target = %q, want %q", got, tt.want)
			}
		})
	}

	t.Run("other pages are not home and link back to it", func(t *testing.T) {
		t.Parallel()
		server := newServer(seededStore(t, testNow), testNow)
		if _, ok := htmltest.Parse(t, renderPath(t, server, "/tickets")).One(t, "main#main").Attr("data-home"); ok {
			t.Errorf("GET /tickets main is marked data-home")
		}
		htmltest.Parse(t, renderPath(t, server, "/insights")).One(t, `main#main a[href="/"]`)
	})

	t.Run("carries the go-to key anchors", func(t *testing.T) {
		t.Parallel()
		root := htmltest.Parse(t, renderPath(t, newServer(seededStore(t, testNow), testNow), "/"))
		for key, href := range map[string]string{
			"g h": "/", "g t": "/tickets", "g i": "/insights", "g r": "/repos", "c": "/launch",
		} {
			root.One(t, `a[data-key="`+key+`"][href="`+href+`"]`)
		}
		root.One(t, `input#rail-search[data-key="mod+k"]`)
		root.One(t, `script[type="module"][src="/assets/dist/keys.js"]`)
	})
}
