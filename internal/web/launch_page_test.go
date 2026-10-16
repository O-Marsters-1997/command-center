package web_test

import (
	"net/http"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/htmltest"
)

func TestFeatureLaunchPage(t *testing.T) {
	t.Parallel()

	t.Run("opens the dialog over the feature page", func(t *testing.T) {
		t.Parallel()
		root := htmltest.Parse(t, renderPath(t, threeFeatureServer(t), "/f/board-scope/launch"))

		root.One(t, `table#board tbody[data-section="ready"]`)
		root.One(t, `dialog#launch-modal-body`).One(t, `cc-launch-modal[feature="board-scope"]`)
	})

	t.Run("the feature page has no dialog until launch is opened", func(t *testing.T) {
		t.Parallel()
		root := htmltest.Parse(t, renderPath(t, threeFeatureServer(t), "/f/board-scope"))

		root.One(t, `table#board`)
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
