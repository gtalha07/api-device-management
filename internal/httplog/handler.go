package httplog

import (
	"context"
	"log/slog"
)

// Handler wraps a slog.Handler and adds the request id from the context to
// every record, so all log lines written while serving a request share it.
// Only calls that pass the context (InfoContext, ErrorContext, ...) get it.
type Handler struct {
	slog.Handler
}

func NewHandler(h slog.Handler) *Handler {
	return &Handler{Handler: h}
}

func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	if id := RequestID(ctx); id != "" {
		r.AddAttrs(slog.String("request_id", id))
	}
	return h.Handler.Handle(ctx, r)
}

// WithAttrs and WithGroup must wrap the result again; the embedded methods
// would return the inner handler, and loggers built with With would silently
// lose the request id.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Handler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h *Handler) WithGroup(name string) slog.Handler {
	return &Handler{Handler: h.Handler.WithGroup(name)}
}
