package cc

import (
	"context"
	"fmt"
	"log"
	"time"
)

// tickPeriod is the sleep *after* work: ticks never overlap, and the loop never branches on
// why it woke.
const tickPeriod = 15 * time.Second

// TickError is the last failed tick, rendered on the page with its age.
type TickError struct {
	At      time.Time `json:"at"`
	Message string    `json:"message"`
}

// Loop is the reconcile loop: observe, decide, act. It is the only writer of the database
// (inv. 9).
type Loop struct {
	store   *Store
	observe ObserveFunc
	now     func() time.Time
}

// NewLoop assembles the loop over an observe phase and a clock.
func NewLoop(store *Store, observe ObserveFunc, now func() time.Time) *Loop {
	return &Loop{store: store, observe: observe, now: now}
}

// RunOnce runs one tick. A failed observe applies no transition and launches nothing: it
// records the error and leaves the last good observation in place, so the page's observe age
// keeps growing (inv. 10). Only a successful observe goes on to apply queued launch intents.
func (l *Loop) RunOnce(ctx context.Context) error {
	obs, err := l.observe(ctx)
	if err != nil {
		at := l.now()
		tickErr := fmt.Errorf("observe: %w", err)
		if recordErr := l.store.RecordTickError(ctx, TickError{At: at, Message: tickErr.Error()}); recordErr != nil {
			return recordErr
		}
		return tickErr
	}

	obs.ObservedAt = l.now()
	if err := l.store.SaveObservation(ctx, obs); err != nil {
		return err
	}
	return l.store.ApplyLaunchIntents(ctx, l.now())
}

// Run ticks until the context is cancelled, sleeping after each tick's work. A tick error is
// already recorded for the page, so the loop logs it and carries on.
func (l *Loop) Run(ctx context.Context) error {
	for {
		if err := l.RunOnce(ctx); err != nil {
			log.Printf("tick: %v", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(tickPeriod):
		}
	}
}
