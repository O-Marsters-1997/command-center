package loop

import (
	"context"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

func (l *Loop) spawnVerifies(
	ctx context.Context, snap plan.Snapshot, t *tick, byURL map[plan.TicketURL]store.Ticket,
) error {
	for _, v := range snap.VerifyAfter(0) {
		ticket, ok := byURL[v.Ticket]
		worktree, held := t.obs.Worktrees[ticket.Ref()]
		argv := t.obs.Settings[ticket.Repo].VerifyCommand
		if !ok || !held || len(argv) == 0 {
			continue
		}
		err := l.spawnRun(ctx, t, spawnSpec{
			ticket: ticket, worktree: worktree, baseline: string(v.Tip), kind: plan.RunKindVerify,
			step: plan.Step{Name: "verify", Command: true}, verify: argv,
		})
		if err != nil {
			return fmt.Errorf("spawn verify in %s: %w", worktree, err)
		}
	}
	return nil
}
