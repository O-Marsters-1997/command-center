package web_test

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

const goldenFeaturePage = "testdata/feature_page.golden.html"

func TestFeaturePageSectionsTheBoardAndNamesDependencies(t *testing.T) {
	t.Parallel()

	page := renderPath(t, threeFeatureServer(t), "/f/board-scope")

	if strings.Contains(page, ticketRef("sandbox://LONE")) {
		t.Errorf("/f/board-scope rendered a ticket from another feature:\n%s", page)
	}
	order := regexp.MustCompile(`data-section="([a-z-]+)"`).FindAllStringSubmatch(page, -1)
	var got []string
	for _, m := range order {
		got = append(got, m[1])
	}
	if want := "ready,blocked"; strings.Join(got, ",") != want {
		t.Errorf("sections = %v, want %s", got, want)
	}
	if cell := rowCellAt(t, page, "sandbox://CHILD", 3); !strings.Contains(cell, "after #ROOT") {
		t.Errorf("CHILD ticket cell = %q, want it to read after #ROOT", cell)
	}
	if cell := rowCellAt(t, page, "sandbox://ROOT", 3); !strings.Contains(cell, "unlocks #CHILD") {
		t.Errorf("ROOT ticket cell = %q, want it to read unlocks #CHILD", cell)
	}
	assertGolden(t, goldenFeaturePage, []byte(page))
}

func TestFeaturePageTabPairMarksTheBoardCurrent(t *testing.T) {
	t.Parallel()

	page := renderPath(t, threeFeatureServer(t), "/f/board-scope")

	for _, want := range []string{
		`<a class="pill-tab" href="/f/board-scope" aria-current="page">Board</a>`,
		`<a class="pill-tab" href="/f/board-scope/graph">Graph</a>`,
		`href="/f/board-scope/launch"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("feature page missing %s:\n%s", want, page)
		}
	}
}

func TestFeaturePageUnknownFeatureIs404(t *testing.T) {
	t.Parallel()

	rec := get(t, threeFeatureServer(t), "/f/bogus")
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /f/bogus: status = %d, want 404", rec.Code)
	}
}
