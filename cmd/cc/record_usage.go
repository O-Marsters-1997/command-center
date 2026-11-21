package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/app"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

const recordUsageTimeout = 3 * time.Second

func recordUsageSubcmd(
	ctx context.Context, logger *slog.Logger, configPath string, env config.Env, _ []string,
) error {
	ctx, cancel := context.WithTimeout(ctx, recordUsageTimeout)
	defer cancel()
	if err := recordUsage(ctx, configPath, env, os.Stdin, time.Now()); err != nil {
		logger.Warn("record usage", "err", err)
	}
	return nil
}

func recordUsage(
	ctx context.Context, configPath string, env config.Env, statusline io.Reader, now time.Time,
) error {
	readings, err := agentlog.ParseStatusline(statusline, now)
	switch {
	case err != nil:
		return fmt.Errorf("parse statusline: %w", err)
	case len(readings) == 0:
		return nil
	}
	err = app.WithStore(configPath, env, func(_ config.Config, st *store.Store) error {
		latest, err := st.LatestReadingsFull(ctx)
		if err != nil {
			return fmt.Errorf("read latest readings: %w", err)
		}
		readings = slices.DeleteFunc(readings, func(r agentlog.Reading) bool {
			prev, ok := latest[r.Window]
			return ok && prev.Utilization == r.Utilization && prev.ResetsAt.Equal(r.ResetsAt)
		})
		if len(readings) == 0 {
			return nil
		}
		if err := st.RecordReadings(ctx, readings); err != nil {
			return fmt.Errorf("record readings: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("record usage readings: %w", err)
	}
	return nil
}
