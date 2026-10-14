package store

import (
	"fmt"
	"slices"
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
)

var allEventKinds = []EventKind{
	EventPushRefused, EventPushFailed, EventPushed, EventImportRefused, EventRemoveWorktreeRefused,
	EventVerdictTransition, EventTickError, EventRefreshRefused, EventRefreshConflicted, EventRefreshed,
	EventRestacked, EventVerificationFailed, EventRetargeted, EventRetargetFailed, EventPROpened,
	EventPRMerged, EventChecksLanded, EventRunLaunched, EventRunDisposed, EventCutFailed,
	EventSpawnFailed, EventWorktreeRemoved, EventLaunchCancelled, EventMergeAborted,
	EventMergeAbortFailed, EventFollowUpRefused, EventDraftReady, EventDraftReadyFailed, EventLaunch,
}

func AllEventKinds() []EventKind { return slices.Clone(allEventKinds) }

func ParseEventKind(s string) (EventKind, error) {
	if k := EventKind(s); slices.Contains(allEventKinds, k) {
		return k, nil
	}
	return EventUnknown, fmt.Errorf("unknown event kind %q", s)
}
