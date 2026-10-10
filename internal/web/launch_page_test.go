package web_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestFeatureLaunchRouteOpensTheDialogOverTheFeaturePage(t *testing.T) {
	t.Parallel()

	page := renderPath(t, threeFeatureServer(t), "/f/board-scope/launch")

	for _, want := range []string{
		`data-section="ready"`,
		`<dialog id="launch-modal-body"`,
		`<cc-launch-modal feature="board-scope">`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("/f/board-scope/launch missing %s:\n%s", want, page)
		}
	}
}

func TestFeaturePageHasNoDialogUntilLaunchIsOpened(t *testing.T) {
	t.Parallel()

	page := renderPath(t, threeFeatureServer(t), "/f/board-scope")

	if strings.Contains(page, "<dialog") {
		t.Errorf("/f/board-scope rendered a dialog:\n%s", page)
	}
}

func TestFeatureLaunchRouteUnknownFeatureIs404(t *testing.T) {
	t.Parallel()

	if rec := get(t, threeFeatureServer(t), "/f/bogus/launch"); rec.Code != http.StatusNotFound {
		t.Errorf("GET /f/bogus/launch: status = %d, want 404", rec.Code)
	}
}

func TestLaunchPickerListsFeaturesWithLaunchableWork(t *testing.T) {
	t.Parallel()

	page := renderPath(t, threeFeatureServer(t), "/launch")

	for _, want := range []string{`href="/f/board-scope/launch"`, `href="/f/sqlc-migration/launch"`} {
		if !strings.Contains(page, want) {
			t.Errorf("/launch missing %s:\n%s", want, page)
		}
	}
}
