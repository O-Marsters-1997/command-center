package loop_test

import "log/slog"

var discardLog = slog.New(slog.DiscardHandler)
