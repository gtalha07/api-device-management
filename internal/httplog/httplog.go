// Package httplog logs one line per HTTP request.
package httplog

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
)

type ctxKey struct{}

// RequestID returns the request id Middleware stored in ctx, or "" if there
// is none.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(ctxKey{}).(string)
	return id
}

// Middleware logs the method, path, status, duration and a request id for
// every request handled by next, and returns the id in the X-Request-ID
// response header so a client can quote it when reporting a problem.
func Middleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := uuid.NewString()
		w.Header().Set("X-Request-ID", id)

		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		ctx := context.WithValue(r.Context(), ctxKey{}, id)
		next.ServeHTTP(rec, r.WithContext(ctx))

		// r.Context() doesn't hold the id, so Handler won't add it a second time.
		logger.InfoContext(r.Context(), "request",
			"request_id", id,
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

// statusRecorder remembers the status code the handler wrote. A handler that
// never calls WriteHeader gets 200, as net/http sends.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if !r.wroteHeader {
		r.status = status
		r.wroteHeader = true
	}

	r.ResponseWriter.WriteHeader(status)
}

// Unwrap lets http.ResponseController reach the real writer, so Flush and
// SetWriteDeadline keep working for the event stream.
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}
