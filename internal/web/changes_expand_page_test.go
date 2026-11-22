package web_test

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/adapter/git"
	"github.com/O-Marsters-1997/command-center/internal/adapter/git/gittest"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web/htmltest"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func threeGapsFixture(t *testing.T) (*storepkg.Store, string) {
	t.Helper()
	return changesFixture(t, func(repo string) string {
		var before, after strings.Builder
		for i := 1; i <= 100; i++ {
			n := strconv.Itoa(i)
			before.WriteString("l" + n + "\n")
			switch i {
			case 30:
				after.WriteString("CHANGED\n")
			case 90:
				after.WriteString("CHANGED90\n")
			case 56:
				after.WriteString("l56\nadded1\nadded2\n")
			default:
				after.WriteString("l" + n + "\n")
			}
		}
		gittest.Commit(t, repo, "big.txt", before.String())
		gittest.Run(t, repo, "update-ref", "refs/remotes/origin/main", "HEAD")
		gittest.Commit(t, repo, "big.txt", after.String())
		return gittest.Run(t, repo, "rev-parse", "cc-1")
	})
}

func contextRows(oldFrom, newFrom, n int) []git.DiffLine {
	rows := make([]git.DiffLine, n)
	for i := range rows {
		rows[i] = git.DiffLine{
			Kind: git.LineContext, OldLine: oldFrom + i, NewLine: newFrom + i, Text: "l" + strconv.Itoa(oldFrom+i),
		}
	}
	return rows
}

func TestReader_Expanded(t *testing.T) {
	t.Parallel()
	st, dataDir := threeGapsFixture(t)
	reader := view.NewReader(st, dataDir, view.ReaderSettings{})
	ref := view.SessionRef{Owner: "acme", Name: "web", N: "1"}

	tests := []struct {
		name string
		e    view.Expand
		want []git.DiffLine
	}{
		{
			name: "above the first hunk takes twenty lines from the top of the file",
			e:    view.Expand{Path: "big.txt", Hunk: 0, Edge: view.ExpandAbove},
			want: contextRows(1, 1, 20),
		},
		{
			name: "below edge of the first gap takes the twenty lines next to the hunk",
			e:    view.Expand{Path: "big.txt", Hunk: 0, Edge: view.ExpandBelow},
			want: contextRows(7, 7, 20),
		},
		{
			name: "all of a gap",
			e:    view.Expand{Path: "big.txt", Hunk: 0, Edge: view.ExpandAll},
			want: contextRows(1, 1, 26),
		},
		{
			name: "a gap of exactly twenty lines closes from the top edge",
			e:    view.Expand{Path: "big.txt", Hunk: 1, Edge: view.ExpandAbove},
			want: contextRows(34, 34, 20),
		},
		{
			name: "a gap of exactly twenty lines closes from the bottom edge",
			e:    view.Expand{Path: "big.txt", Hunk: 1, Edge: view.ExpandBelow},
			want: contextRows(34, 34, 20),
		},
		{
			name: "after a hunk that adds lines the old numbers trail the new by two, from the top edge",
			e:    view.Expand{Path: "big.txt", Hunk: 2, Edge: view.ExpandAbove},
			want: contextRows(60, 62, 20),
		},
		{
			name: "after a hunk that adds lines the old numbers trail the new by two, from the bottom edge",
			e:    view.Expand{Path: "big.txt", Hunk: 2, Edge: view.ExpandBelow},
			want: contextRows(67, 69, 20),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := reader.Expanded(t.Context(), testNow, ref, tt.e)
			if err != nil {
				t.Fatalf("Expanded(%+v) error = %v", tt.e, err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Expanded(%+v) mismatch (-want +got):\n%s", tt.e, diff)
			}
		})
	}

	t.Run("repeated clicks take twenty at a time until the gap closes", func(t *testing.T) {
		t.Parallel()
		e := view.Expand{Path: "big.txt", Hunk: 0, Edge: view.ExpandAbove}
		first, err := reader.ExpandMore(t.Context(), testNow, ref, e, 0, 0)
		if err != nil || len(first.Rows) != 20 || first.Gap.Remaining != 6 {
			t.Fatalf("ExpandMore(above) = %d rows, %d left, %v, want 20 rows and 6 left",
				len(first.Rows), first.Gap.Remaining, err)
		}
		e.Edge = view.ExpandBelow
		second, err := reader.ExpandMore(t.Context(), testNow, ref, e, first.Gap.Above, first.Gap.Below)
		if err != nil {
			t.Fatalf("ExpandMore(below) error = %v", err)
		}
		if got := second.Rows[0].NewLine; got != 21 || len(second.Rows) != 6 || second.Gap.Remaining != 0 {
			t.Errorf("ExpandMore(below) = %d rows from %d, %d left, want 6 rows from 21 and none left",
				len(second.Rows), got, second.Gap.Remaining)
		}
	})

	t.Run("an unknown hunk, path or edge is no such line", func(t *testing.T) {
		t.Parallel()
		for _, e := range []view.Expand{
			{Path: "big.txt", Hunk: 9, Edge: view.ExpandAll},
			{Path: "nope.txt", Hunk: 0, Edge: view.ExpandAll},
			{Path: "big.txt", Hunk: 0, Edge: "sideways"},
		} {
			if _, err := reader.Expanded(t.Context(), testNow, ref, e); !errors.Is(err, view.ErrNoSuchLine) {
				t.Errorf("Expanded(%+v) error = %v, want ErrNoSuchLine", e, err)
			}
		}
	})
}

func TestChangesPage_expand(t *testing.T) {
	t.Parallel()

	t.Run("a gap offers directional buttons that swap in rows, then offers the rest", func(t *testing.T) {
		t.Parallel()
		st, dataDir := threeGapsFixture(t)
		server := openServer(st, fixedClock(testNow), dataDir)

		page := htmltest.Parse(t, renderPath(t, server, "/s/acme/web/1/changes"))
		gaps := page.Find("tr.diff-gap")
		if len(gaps) != 3 || !strings.Contains(gaps[0].Text(), "26 hidden lines") {
			t.Fatalf("GET changes gaps = %d, want 3 with the first reading 26 hidden lines", len(gaps))
		}
		if got := len(page.Find(".diff-gap-btn")); got != 6 {
			t.Errorf("GET changes gap buttons = %d, want 2 per gap", got)
		}

		var target string
		for _, b := range page.Find(".diff-gap-btn") {
			if hx, _ := b.Attr("hx-get"); strings.Contains(hx, "hunk=0") && strings.Contains(hx, "edge=above") {
				target = hx
			}
		}
		if target == "" {
			t.Fatal("GET changes has no expand-above button on the first gap")
		}
		rec := do(t, server, http.MethodGet, target, req{})
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", target, rec.Code)
		}
		body := htmltest.Parse(t, "<table><tbody>"+rec.Body+"</tbody></table>")
		if got := len(body.Find("tr.diff-line.diff-context")); got != 20 {
			t.Errorf("GET %s context rows = %d, want 20", target, got)
		}
		left := body.One(t, "tr.diff-gap")
		if !strings.Contains(left.Text(), "6 hidden lines") || !strings.Contains(left.HTML(), "Expand all") {
			t.Errorf("GET %s remaining gap = %q, want 6 hidden lines with Expand all", target, left.Text())
		}
	})

	t.Run("a comment on a hidden line persists and shows when the gap is expanded", func(t *testing.T) {
		t.Parallel()
		st, dataDir := threeGapsFixture(t)
		server := openServer(st, fixedClock(testNow), dataDir)

		form := url.Values{"path": {"big.txt"}, "side": {"new"}, "line": {"5"}, "body": {"why five"}}
		posted := do(t, server, http.MethodPost, "/s/acme/web/1/changes/comments", req{Form: form})
		if posted.Code != http.StatusOK {
			t.Fatalf("POST comment on a hidden line = %d %q, want 200", posted.Code, posted.Body)
		}
		rec := do(t, server, http.MethodGet,
			"/s/acme/web/1/changes/expand?path=big.txt&hunk=0&edge=all&above=0&below=0", req{})
		if !strings.Contains(rec.Body, "why five") {
			t.Errorf("GET expand body = %q, want the pending comment on line 5", rec.Body)
		}
	})
}
