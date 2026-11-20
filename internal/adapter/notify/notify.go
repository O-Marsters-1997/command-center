package notify

import (
	"context"
	"log/slog"
)

type Log struct{ Logger *slog.Logger }

func (l Log) Notify(_ context.Context, title, body string) error {
	l.Logger.Warn(title, "body", body)
	return nil
}
