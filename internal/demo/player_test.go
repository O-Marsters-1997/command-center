//go:build demo

package demo

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/store"
)

func newTestPlayer(t *testing.T) *Player { return newTestPlayerUntil(t, 0) }

func newTestPlayerUntil(t *testing.T, until time.Duration) *Player {
	t.Helper()
	sc, err := LoadScenario(filepath.Join("..", "..", "demo", "scenarios", "happy.toml"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewPlayer(t.Context(), sc, 0, until)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := p.Close(); err != nil {
			t.Errorf("close player: %v", err)
		}
	})
	return p
}

func post(t *testing.T, p *Player, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST %s = %d: %s", path, rec.Code, rec.Body)
	}
	return rec
}

func step(t *testing.T, p *Player) time.Duration {
	t.Helper()
	wait, err := p.step(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return wait
}

func TestPauseStopsTicksAndResumeContinuesFromTheSameSimTime(t *testing.T) {
	p := newTestPlayer(t)
	step(t, p)
	step(t, p)
	before := p.Status().Elapsed

	post(t, p, "/dev/pause", nil)
	if wait := step(t, p); wait != untilControlChange {
		t.Errorf("step while paused waited %v, want to wait for a control change", wait)
	}
	if got := p.Status().Elapsed; got != before {
		t.Errorf("Elapsed after a paused step = %v, want %v", got, before)
	}

	post(t, p, "/dev/resume", nil)
	step(t, p)
	if got, want := p.Status().Elapsed, before+store.TickPeriod; got != want {
		t.Errorf("Elapsed after resume = %v, want %v", got, want)
	}
}

func TestJumpPlaysForwardWithoutWaiting(t *testing.T) {
	p := newTestPlayer(t)
	post(t, p, "/dev/jump", url.Values{"to": {"2m"}})
	for p.Status().Elapsed < 2*time.Minute {
		if wait := step(t, p); wait != goAgain {
			t.Fatalf("step during a jump waited %v, want none", wait)
		}
	}
}

func TestRestartReplaysFromZero(t *testing.T) {
	p := newTestPlayer(t)
	for range 8 {
		step(t, p)
	}
	first := p.current().Transitions()
	if len(first) == 0 {
		t.Fatal("no transitions before restart")
	}

	post(t, p, "/dev/restart", nil)
	step(t, p)
	if got := p.Status().Elapsed; got != 0 {
		t.Errorf("Elapsed after restart = %v, want 0", got)
	}
	for range 8 {
		step(t, p)
	}
	if second := p.current().Transitions(); !slices.Equal(first, second) {
		t.Errorf("replay differs from first play:\nfirst:  %v\nsecond: %v", first, second)
	}
}

func TestMergeNowMergesThroughTheForgeOnTheNextTick(t *testing.T) {
	p := newTestPlayer(t)
	for len(p.Status().PRs) == 0 {
		step(t, p)
		if p.Status().Elapsed > time.Hour {
			t.Fatal("no pull request became ready")
		}
	}
	pr := p.Status().PRs[0]

	post(t, p, "/dev/merge-now", url.Values{"number": {strconv.Itoa(pr.Number)}})
	step(t, p)
	if slices.Contains(p.Status().PRs, pr) {
		t.Errorf("pull request #%d still open after merge-now and a tick", pr.Number)
	}
}

func TestTheStripIsInjectedOutsideTheBoardAndOnlyIntoFullPages(t *testing.T) {
	p := newTestPlayer(t)
	step(t, p)

	page := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "text/html")
	p.Handler().ServeHTTP(page, req)
	body := page.Body.String()
	strip, board := strings.Index(body, `id="dev-strip"`), strings.Index(body, `id="board"`)
	if strip < 0 || board < 0 || strip < board {
		t.Fatalf("strip at %d, board at %d, want the strip after the board", strip, board)
	}

	fragment := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/board", nil)
	req.Header.Set("Accept", "text/html")
	req.Header.Set("HX-Request", "true")
	p.Handler().ServeHTTP(fragment, req)
	if strings.Contains(fragment.Body.String(), "dev-strip") {
		t.Error("board fragment carries the dev strip")
	}
}

func TestResumeAfterUntilKeepsPlaying(t *testing.T) {
	p := newTestPlayerUntil(t, 30*time.Second)
	for !p.Status().Paused {
		step(t, p)
	}
	stopped := p.Status().Elapsed

	post(t, p, "/dev/resume", nil)
	step(t, p)
	step(t, p)
	if got := p.Status().Elapsed; got <= stopped {
		t.Errorf("Elapsed after resume = %v, want past %v", got, stopped)
	}
	if p.Status().Paused {
		t.Error("run paused again after resume")
	}
}

func TestMergeNowWhilePausedMergesWithoutResuming(t *testing.T) {
	p := newTestPlayer(t)
	for len(p.Status().PRs) == 0 {
		step(t, p)
	}
	pr := p.Status().PRs[0]
	post(t, p, "/dev/pause", nil)

	post(t, p, "/dev/merge-now", url.Values{"number": {strconv.Itoa(pr.Number)}})
	step(t, p)
	if slices.Contains(p.Status().PRs, pr) {
		t.Errorf("pull request #%d still open after merge-now while paused", pr.Number)
	}
	if !p.Status().Paused {
		t.Error("merge-now resumed the run")
	}
}

func TestBadInputIsShownInTheStrip(t *testing.T) {
	p := newTestPlayer(t)
	for _, bad := range []string{"NaN", "Inf", "0", "-1", "fast"} {
		rec := post(t, p, "/dev/speed", url.Values{"speed": {bad}})
		if !strings.Contains(rec.Body.String(), `role="alert"`) {
			t.Errorf("speed %q: strip shows no error: %s", bad, rec.Body)
		}
	}
	if got := p.Status().Speed; got != defaultSpeed {
		t.Errorf("Speed after bad input = %v, want %v", got, float64(defaultSpeed))
	}
}
