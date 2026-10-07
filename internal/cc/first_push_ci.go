package cc

import (
	"context"
	"strings"
)

func (l *Loop) recordFirstPushCI(ctx context.Context) error {
	firstPush, err := l.store.FirstPushedAt(ctx)
	if err != nil {
		return err
	}
	events, err := l.store.VerdictTransitionEvents(ctx)
	if err != nil {
		return err
	}

	resolved := map[string]bool{}
	for _, e := range events {
		if resolved[e.TicketURL] {
			continue
		}
		pushedAt, ok := firstPush[e.TicketURL]
		if !ok || e.At.Before(pushedAt) {
			continue
		}
		passed, terminal := terminalCIVerdict(e.Detail)
		if !terminal {
			continue
		}

		resolved[e.TicketURL] = true
		if err := l.store.SetFirstPushCI(ctx, e.TicketURL, passed); err != nil {
			return err
		}
	}
	return nil
}

// terminalCIVerdict switches on plan.VerdictLabel's own vocabulary.
func terminalCIVerdict(detail string) (passed, terminal bool) {
	label, _, _ := strings.Cut(detail, ":")
	switch label {
	case "review_me":
		return true, true
	case "ci_failed":
		return false, true
	default:
		return false, false
	}
}
