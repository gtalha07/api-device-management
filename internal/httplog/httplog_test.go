package httplog

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMiddlewareLogsRequest(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	rec := httptest.NewRecorder()
	Middleware(logger, next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/devices/abc", nil))

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("log line is not JSON: %v\n%s", err, buf.String())
	}
	want := map[string]any{"msg": "request", "method": "GET", "path": "/devices/abc", "status": float64(404)}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("%s = %v, want %v", key, got[key], value)
		}
	}
	id := rec.Header().Get("X-Request-ID")
	if id == "" || got["request_id"] != id {
		t.Errorf("request_id = %v, X-Request-ID = %q; want the same non-empty id", got["request_id"], id)
	}
}

func TestMiddlewareDefaultsToOK(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok")) // no WriteHeader: net/http sends 200
	})

	Middleware(logger, next).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))

	var got struct{ Status int }
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("log line is not JSON: %v", err)
	}
	if got.Status != http.StatusOK {
		t.Errorf("status = %d, want 200", got.Status)
	}
}

// The event stream flushes and clears its write deadline through
// http.ResponseController; both must still reach the real writer.
func TestMiddlewareKeepsResponseController(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		rc := http.NewResponseController(w)
		if err := rc.SetWriteDeadline(time.Time{}); err != nil {
			t.Errorf("SetWriteDeadline: %v", err)
		}
		if err := rc.Flush(); err != nil {
			t.Errorf("Flush: %v", err)
		}
	})

	srv := httptest.NewServer(Middleware(logger, next))
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()
}
