package loop

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

const (
	exploreModel = "claude-haiku-5-5"
	agentModel   = "claude-sonnet-5-5"
)

func modelFor(kind string) string {
	if kind == runKindExplore {
		return exploreModel
	}
	return agentModel
}

func (l *Loop) launchDir(launchID int64) string {
	return filepath.Join(l.ws.RunsDir, fmt.Sprintf("launch-%d", launchID))
}

func (l *Loop) briefPath(launchID int64) string {
	return filepath.Join(l.launchDir(launchID), "brief.md")
}

func (l *Loop) exploreTree(launchID int64) string {
	return filepath.Join(l.launchDir(launchID), "tree")
}

type exploreState struct {
	runs     map[int64]store.ExploreRun
	members  map[int64][]string
	launchOf map[string]int64
}

func (l *Loop) exploreState(ctx context.Context) (*exploreState, error) {
	runs, err := l.store.ExploreRuns(ctx)
	if err != nil {
		return nil, err
	}
	members, err := l.store.ActiveLaunchTickets(ctx)
	if err != nil {
		return nil, err
	}
	launchOf := map[string]int64{}
	for launchID, urls := range members {
		for _, url := range urls {
			launchOf[url] = launchID
		}
	}
	return &exploreState{runs: runs, members: members, launchOf: launchOf}, nil
}

func (l *Loop) briefFor(
	ctx context.Context, st *exploreState, ticket store.Ticket, baseBranch string, byTicket map[string]store.Ticket,
) (path string, ready bool, err error) {
	launchID, ok := st.launchOf[ticket.URL]
	if !ok {
		return "", true, nil
	}
	run, started := st.runs[launchID]
	if !started {
		st.runs[launchID] = store.ExploreRun{}
		return "", false, l.startExplore(ctx, launchID, baseBranch, st.members[launchID], byTicket)
	}
	if !run.Disposed {
		return "", false, nil
	}
	if _, err := os.Stat(l.briefPath(launchID)); err != nil {
		return "", true, nil
	}
	return l.briefPath(launchID), true, nil
}

func (l *Loop) startExplore(
	ctx context.Context, launchID int64, baseBranch string, urls []string, byTicket map[string]store.Ticket,
) error {
	var members []plan.Ticket
	var first store.Ticket
	for _, url := range urls {
		t, ok := byTicket[url]
		if !ok {
			continue
		}
		if len(members) == 0 {
			first = t
		}
		members = append(members, t.Plan())
	}
	if len(members) == 0 {
		return nil
	}

	if err := os.MkdirAll(l.launchDir(launchID), 0o700); err != nil {
		return fmt.Errorf("create dir for launch %d: %w", launchID, err)
	}
	tree := l.exploreTree(launchID)
	if err := git.AddDetached(ctx, l.checkout(first.Repo), tree, "origin/"+baseBranch); err != nil {
		return l.store.InsertCutFailedExploreRun(ctx, launchID, l.clock.Now())
	}
	return l.spawnRun(ctx, spawnSpec{
		launchID: launchID, worktree: tree, kind: runKindExplore,
		prompt: plan.ComposeExplore(members, l.briefPath(launchID)),
	})
}

func (l *Loop) removeExploreWorktree(ctx context.Context, launchID int64) {
	urls, err := l.store.LaunchMemberTickets(ctx, launchID)
	if err != nil || len(urls) == 0 {
		return
	}
	first, ok, err := l.ticket(ctx, urls[0])
	if err != nil || !ok {
		return
	}
	if err := git.RemoveDetached(ctx, l.checkout(first.Repo), l.exploreTree(launchID)); err != nil {
		log.Printf("remove explore worktree for launch %d: %v", launchID, err)
	}
}
