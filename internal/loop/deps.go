package loop

import (
	"context"
	"log/slog"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/tp"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
)

type forge interface {
	List(ctx context.Context, repoPath string, tracked []string) (gh.Snapshot, error)
	IssueTitles(ctx context.Context, repoPath string) (map[string]string, error)
	Create(ctx context.Context, repoPath, base, body string, draft bool) error
	Ready(ctx context.Context, repoPath, branch string) error
	Edit(ctx context.Context, repoPath, branch, base string) error
	CloseIssue(ctx context.Context, repoPath, issueURL string) error
}

type worktrees interface {
	New(ctx context.Context, repoPath, branch, baseRef string) error
	Remove(ctx context.Context, repoPath, branch string, mode tp.RemoveMode) error
}

type Deps struct {
	Logger    *slog.Logger
	Store     *store.Store
	Clock     Clock
	Runner    runner.Runner
	Observe   ObserveFunc
	Config    config.Config
	Workspace config.Workspace
	Validate  ValidateFunc
	RemoteOf  RemoteFunc
	Tracker   tracker.Resolver
}

type Option func(*Loop)

func WithForge(f forge) Option { return func(l *Loop) { l.forge = f } }

func WithWorktrees(w worktrees) Option { return func(l *Loop) { l.worktrees = w } }

func New(d Deps, opts ...Option) *Loop {
	l := &Loop{
		logger: d.Logger, store: d.Store, observe: d.Observe, clock: d.Clock, runner: d.Runner,
		cfg: d.Config, ws: d.Workspace,
		forge:      gh.CLI{},
		worktrees:  tp.CLI{},
		validate:   ValidateRepo,
		remoteOf:   gh.CLI{}.RepoRemote,
		trackerFor: tracker.Resolve(gh.CLI{}),
		nudgeCh:    make(chan struct{}, 1),
	}
	for _, opt := range opts {
		opt(l)
	}
	if d.Validate != nil {
		l.validate = d.Validate
	}
	if d.RemoteOf != nil {
		l.remoteOf = d.RemoteOf
	}
	if d.Tracker != nil {
		l.trackerFor = d.Tracker
	}
	return l
}

type Nudger struct{ ch chan<- struct{} }

func (n Nudger) Nudge() {
	select {
	case n.ch <- struct{}{}:
	default:
	}
}

func (l *Loop) Nudger() Nudger { return Nudger{ch: l.nudgeCh} }

type tick struct {
	now       time.Time
	obs       plan.Observation
	prev      plan.Observation
	spawned   []plan.TicketURL
	cancelled map[int]bool
}
