package store

import (
	"context"
	"fmt"
	"slices"

	"github.com/O-Marsters-1997/command-center/internal/plan/insight"
	"github.com/O-Marsters-1997/command-center/internal/store/ccdb"
)

type EventKind string

const (
	EventUnknown               EventKind = ""
	EventPushRefused           EventKind = "push_refused"
	EventPushFailed            EventKind = "push_failed"
	EventPushed                EventKind = "pushed"
	EventImportRefused         EventKind = "import_refused"
	EventRemoveWorktreeRefused EventKind = "remove_worktree_refused"
	EventVerdictTransition     EventKind = "verdict_transition"
	EventTickError             EventKind = "tick_error"
	EventRefreshRefused        EventKind = "refresh_refused"
	EventRefreshConflicted     EventKind = "refresh_conflicted"
	EventRefreshed             EventKind = "refreshed"
	EventRestacked             EventKind = "restacked"
	EventVerificationFailed    EventKind = "verification_failed"
	EventRetargeted            EventKind = "retargeted"
	EventRetargetFailed        EventKind = "retarget_failed"
	EventPROpened              EventKind = "pr_opened"
	EventPRMerged              EventKind = "pr_merged"
	EventChecksLanded          EventKind = "checks_landed"
	EventRunLaunched           EventKind = "run_launched"
	EventRunDisposed           EventKind = "run_disposed"
	EventCutFailed             EventKind = "cut_failed"
	EventSpawnFailed           EventKind = "spawn_failed"
	EventWorktreeRemoved       EventKind = "worktree_removed"
	EventLaunchCancelled       EventKind = "launch_cancelled"
	EventMergeAborted          EventKind = "merge_aborted"
	EventMergeAbortFailed      EventKind = "merge_abort_failed"
	EventFollowUpRefused       EventKind = "follow_up_refused"
	EventDraftReady            EventKind = "draft_ready"
	EventDraftReadyFailed      EventKind = "draft_ready_failed"
	EventLaunch                EventKind = "launch"
	EventResolveStarted        EventKind = "resolve_started"
	EventResolveRefused        EventKind = "resolve_refused"
	EventRaiseRefused          EventKind = "raise_refused"
	EventCILogUnavailable      EventKind = "ci_log_unavailable"
	EventParked                EventKind = "parked"
	EventStageEntered          EventKind = "stage_entered"
)

var allEventKinds = []EventKind{
	EventPushRefused, EventPushFailed, EventPushed, EventImportRefused, EventRemoveWorktreeRefused,
	EventVerdictTransition, EventTickError, EventRefreshRefused, EventRefreshConflicted, EventRefreshed,
	EventRestacked, EventVerificationFailed, EventRetargeted, EventRetargetFailed, EventPROpened,
	EventPRMerged, EventChecksLanded, EventRunLaunched, EventRunDisposed, EventCutFailed,
	EventSpawnFailed, EventWorktreeRemoved, EventLaunchCancelled, EventMergeAborted,
	EventMergeAbortFailed, EventFollowUpRefused, EventDraftReady, EventDraftReadyFailed, EventLaunch,
	EventResolveStarted, EventResolveRefused, EventRaiseRefused, EventCILogUnavailable,
	EventParked, EventStageEntered,
}

func (s *Store) RecordRefreshConflict(ctx context.Context, ticket, detail string, paths []insight.ConflictPath) error {
	type row struct {
		Path      string `json:"path"`
		Generated bool   `json:"generated"`
	}
	rows := make([]row, len(paths))
	for i, p := range paths {
		rows[i] = row{Path: p.Path, Generated: p.Generated}
	}
	encoded, err := encodeJSON("conflict paths", rows)
	if err != nil {
		return fmt.Errorf("record %s for %s: %w", EventRefreshConflicted, ticket, err)
	}
	err = s.inTx(ctx, func(q *ccdb.Queries) error {
		id, err := q.AppendEventReturningID(ctx, ccdb.AppendEventReturningIDParams{
			TicketID: nullIfEmpty(ticket), Kind: string(EventRefreshConflicted), Detail: detail,
		})
		if err != nil {
			return fmt.Errorf("append event: %w", err)
		}
		if err := q.InsertConflictPaths(ctx, ccdb.InsertConflictPathsParams{EventID: id, Paths: encoded}); err != nil {
			return fmt.Errorf("insert conflict paths: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("record %s for %s: %w", EventRefreshConflicted, ticket, err)
	}
	return nil
}

func AllEventKinds() []EventKind { return slices.Clone(allEventKinds) }

func ParseEventKind(s string) (EventKind, error) {
	if k := EventKind(s); slices.Contains(allEventKinds, k) {
		return k, nil
	}
	return EventUnknown, fmt.Errorf("unknown event kind %q", s)
}
