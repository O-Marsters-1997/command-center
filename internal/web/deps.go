package web

import (
	"context"
	"log/slog"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/adapter/gh"
	"github.com/O-Marsters-1997/command-center/internal/adapter/tracker"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

type Nudger interface{ Nudge() }

type Clock interface{ Now() time.Time }

type Deps struct {
	Logger           *slog.Logger
	Store            *store.Store
	Clock            Clock
	DataDir          string
	Nudger           Nudger
	SpendLimit5h     int
	BoardPollSeconds int
}

type Option func(*Server)

func WithTrackerSource(r tracker.Resolver) Option { return func(s *Server) { s.trackerFor = r } }

func WithPushableSource(list func(context.Context) ([]gh.RepoSummary, error)) Option {
	return func(s *Server) { s.pushable.list = list }
}
