package client

import (
	"context"
	"log/slog"
	"strings"
	"time"
)

// lineWriter is the standard log package's output: each line becomes an
// INFO record on the fan-out, so legacy log.Printf callers reach both
// sinks (and are redacted) without changing a line of code.
type lineWriter struct {
	h slog.Handler
}

func (w lineWriter) Write(p []byte) (int, error) {
	ctx := context.Background()
	if !w.h.Enabled(ctx, slog.LevelInfo) {
		return len(p), nil
	}
	for _, line := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		if err := w.h.Handle(ctx, slog.NewRecord(time.Now(), slog.LevelInfo, line, 0)); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}
