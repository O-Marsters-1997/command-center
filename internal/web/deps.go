package web

import (
	"context"
	"log/slog"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
)

type Nudger interface{ Nudge() }

type Clock interface{ Now() time.Time }

type Deps struct {
	Logger  *slog.Logger
	Store   *store.Store
	Clock   Clock
	DataDir string
	Nudger  Nudger
}

type Option func(*Server)

func WithTrackerSource(r tracker.Resolver) Option { return func(s *Server) { s.trackerFor = r } }

func WithPushableSource(list func(context.Context) ([]gh.RepoSummary, error)) Option {
	return func(s *Server) { s.pushable.list = list }
}

func WithSpendLimit5h(pct int) Option { return func(s *Server) { s.spendLimit5h = pct } }

func WithBoardPollSeconds(n int) Option { return func(s *Server) { s.boardPollSeconds = n } }

func WithLocation(loc *time.Location) Option { return func(s *Server) { s.loc = loc } }
