package web_test

import (
	"net/http"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/htmltest"
)

func TestFeatureLaunchRouteOpensTheDialogOverTheFeaturePage(t *testing.T) {
	t.Parallel()

	page := renderPath(t, threeFeatureServer(t), "/f/board-scope/launch")

	root := htmltest.Parse(t, page)
	root.One(t, `table#board tbody[data-section="ready"]`)
	dialog := root.One(t, `dialog#launch-modal-body`)
	dialog.One(t, `cc-launch-modal[feature="board-scope"]`)
}

func TestFeaturePageHasNoDialogUntilLaunchIsOpened(t *testing.T) {
	t.Parallel()

	page := renderPath(t, threeFeatureServer(t), "/f/board-scope")

	root := htmltest.Parse(t, page)
	root.One(t, `table#board`)
	if dialogs := root.Find(`dialog#launch-modal-body`); len(dialogs) != 0 {
		t.Errorf("/f/board-scope rendered the launch dialog: %s", dialogs[0].HTML())
	}
}

func TestFeatureLaunchRouteUnknownFeatureIs404(t *testing.T) {
	t.Parallel()

	if rec := do(t, threeFeatureServer(t), http.MethodGet, "/f/bogus/launch", req{}); rec.Code != http.StatusNotFound {
		t.Errorf("GET /f/bogus/launch: status = %d, want 404", rec.Code)
	}
}

func TestLaunchPickerListsFeaturesWithLaunchableWork(t *testing.T) {
	t.Parallel()

	page := renderPath(t, threeFeatureServer(t), "/launch")

	root := htmltest.Parse(t, page)
	for _, href := range []string{"/f/board-scope/launch", "/f/sqlc-migration/launch"} {
		root.One(t, `a[href="`+href+`"]`)
	}
}
