package log

import (
	"context"
	"log/slog"
)

type loggerKey struct{}

// FromCtx returns the slog.FromCtx associated with the ctx.
// If no logger is associated, or the logger or ctx are nil,
// slog.Default() is returned.
func FromCtx(ctx context.Context) *slog.Logger {
	if ctx == nil {
		return slog.Default()
	}

	if l, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok {
		return l
	}

	return slog.Default()
}

// NewCtx returns a copy of ctx with the logger attached.
func NewCtx(parentCtx context.Context, l *slog.Logger) context.Context {
	if parentCtx == nil {
		parentCtx = context.Background()
	}

	return context.WithValue(parentCtx, loggerKey{}, l)
}
