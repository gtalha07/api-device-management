// Package httplog logs one line per HTTP request.
package httplog

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// Middleware logs the method, path, status, duration and a request id for
// every request handled by next, and returns the id in the X-Request-ID
// response header so a client can quote it when reporting a problem.
//
// TODO: the request id is not passed to the handlers' own logs; putting it
// in the request context would let every log line of a request share it.
func Middleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := uuid.NewString()
		w.Header().Set("X-Request-ID", id)

		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

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
