package view

import (
	"context"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cctest"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

type countingFrameStore struct {
	*store.Store
	repos int
}

func (s *countingFrameStore) Repos(ctx context.Context) ([]store.Repo, error) {
	s.repos++
	return s.Store.Repos(ctx)
}

func TestBoardRailAndHomeEachReadReposAndDeriveOnce(t *testing.T) {
	st, err := store.OpenStore(cctest.DSN(t))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	reads := &countingFrameStore{Store: st}
	r := NewReader(st, "", nil, 5, 0)
	r.frameStore = reads
	derives := 0
	r.derive = func(in plan.Input) plan.Snapshot {
		derives++
		return plan.RulesFor(plan.Daemon{}, in.Obs).Derive(in)
	}
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		render func() error
	}{
		{"board", func() error { _, err := r.Board(t.Context(), now, Params{}); return err }},
		{"rail", func() error { _, err := r.Rail(t.Context(), now, RailParams{}); return err }},
		{"home", func() error { _, err := r.Home(t.Context(), now); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			beforeRepos, beforeDerives := reads.repos, derives
			if err := tc.render(); err != nil {
				t.Fatalf("render %s: %v", tc.name, err)
			}
			if got := reads.repos - beforeRepos; got != 1 {
				t.Errorf("%s Repos reads = %d, want 1", tc.name, got)
			}
			if got := derives - beforeDerives; got != 1 {
				t.Errorf("%s Derive calls = %d, want 1", tc.name, got)
			}
		})
	}
}
