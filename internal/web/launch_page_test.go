package web_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
	"github.com/O-Marsters-1997/command-center/internal/web/htmltest"
)

func TestFeatureLaunchPage(t *testing.T) {
	t.Parallel()

	t.Run("opens the dialog over the feature page", func(t *testing.T) {
		t.Parallel()
		root := htmltest.Parse(t, renderPath(t, threeFeatureServer(t), "/f/board-scope/launch"))

		root.One(t, `div#board section[data-section="ready"]`)
		root.One(t, `dialog#launch-modal-body`).One(t, `cc-launch-modal[feature="board-scope"]`)
	})

	t.Run("the feature page has no dialog until launch is opened", func(t *testing.T) {
		t.Parallel()
		root := htmltest.Parse(t, renderPath(t, threeFeatureServer(t), "/f/board-scope"))

		root.One(t, `div#board`)
		if dialogs := root.Find(`dialog#launch-modal-body`); len(dialogs) != 0 {
			t.Errorf("GET /f/board-scope rendered the launch dialog: %s", dialogs[0].HTML())
		}
	})

	t.Run("an unknown feature is 404", func(t *testing.T) {
		t.Parallel()
		if rec := do(t, threeFeatureServer(t), http.MethodGet, "/f/bogus/launch", req{}); rec.Code != http.StatusNotFound {
			t.Errorf("GET /f/bogus/launch status = %d, want 404", rec.Code)
		}
	})
}

func TestLaunchPickerPage(t *testing.T) {
	t.Parallel()

	root := htmltest.Parse(t, renderPath(t, threeFeatureServer(t), "/launch"))
	for _, href := range []string{"/f/board-scope/launch", "/f/sqlc-migration/launch"} {
		root.One(t, `a[href="`+href+`"]`)
	}
}

func TestLaunchDialogProject(t *testing.T) {
	t.Parallel()
	st := track(t, openStore(t), alphaRepo)
	storetest.Tickets(t, st, storepkg.Ticket{URL: acmeTicket, Repo: "acme/alpha", Branch: "one", Feature: "Billing"})
	server := openServer(st, fixedClock(testNow), "")
	form := url.Values{"name": {"acme"}, "hue": {"1"}, "repo": {"acme/alpha"}}
	if rec := do(t, server, http.MethodPost, "/projects", req{Form: form}); rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /projects status = %d, want 303", rec.Code)
	}

	t.Run("the feature dialog names its project and offers Change feature", func(t *testing.T) {
		t.Parallel()
		dialog := htmltest.Parse(t, renderPath(t, server, "/f/Billing/launch")).One(t, "dialog#launch-modal-body")
		dialog.One(t, `.launch-proj [data-hue="1"][title="acme"]`)
		dialog.One(t, `a[href="/launch"][hx-get="/launch"]`)
	})

	t.Run("the picker row carries the project's avatar and counts", func(t *testing.T) {
		t.Parallel()
		row := htmltest.Parse(t, renderPath(t, server, "/launch")).One(t, `.launch-picks a[href="/f/Billing/launch"]`)
		row.One(t, `[data-hue="1"][title="acme"]`)
		if got := row.One(t, ".launch-pick-n").Text(); !strings.Contains(got, "1 ready") {
			t.Errorf("GET /launch Billing counts = %q, want 1 ready", got)
		}
	})
}
