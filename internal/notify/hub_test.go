package notify

import (
	"bufio"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gtalha07/api-device-management/internal/device"
)

func newTestHub() *Hub {
	return NewHub(slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func testChange(id string) device.StateChange {
	return device.StateChange{
		DeviceID:  id,
		Previous:  device.StateAvailable,
		Current:   device.StateInUse,
		ChangedAt: time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC),
	}
}

func openStream(t *testing.T, url string) (http.Header, io.Reader) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp.Header, resp.Body
}

func TestHubNotifyFansOut(t *testing.T) {
	h := newTestHub()
	a, _ := h.subscribe()
	b, _ := h.subscribe()

	change := testChange("d1")
	if err := h.Notify(t.Context(), change); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	for name, ch := range map[string]chan device.StateChange{"a": a, "b": b} {
		select {
		case got := <-ch:
			if got != change {
				t.Errorf("subscriber %s got %+v, want %+v", name, got, change)
			}
		default:
			t.Errorf("subscriber %s got nothing", name)
		}
	}
}

func TestHubNotifyDoesNotBlockOnSlowSubscriber(t *testing.T) {
	h := newTestHub()
	slow, _ := h.subscribe()

	// One more than the buffer holds: the last one must be dropped, not block.
	for i := range subscriberBuffer + 1 {
		if err := h.Notify(t.Context(), testChange(string(rune('a'+i)))); err != nil {
			t.Fatalf("Notify: %v", err)
		}
	}
	if got := len(slow); got != subscriberBuffer {
		t.Errorf("buffered events = %d, want %d", got, subscriberBuffer)
	}
}

func TestHubUnsubscribe(t *testing.T) {
	h := newTestHub()
	ch, _ := h.subscribe()
	h.unsubscribe(ch)

	if err := h.Notify(t.Context(), testChange("d1")); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if len(ch) != 0 {
		t.Error("unsubscribed channel still received an event")
	}
}

func TestHubClosedRefusesSubscribers(t *testing.T) {
	h := newTestHub()
	h.Close()
	h.Close() // safe to call twice

	if _, ok := h.subscribe(); ok {
		t.Error("subscribe after Close succeeded, want refused")
	}
}

func TestHubServeHTTPStreamsEvents(t *testing.T) {
	h := newTestHub()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	t.Cleanup(h.Close)

	header, body := openStream(t, srv.URL)

	if ct := header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	// The handler subscribes before sending headers, so this is not lost.
	change := testChange("d1")
	if err := h.Notify(t.Context(), change); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	line, err := bufio.NewReader(body).ReadString('\n')
	if err != nil {
		t.Fatalf("read event: %v", err)
	}
	data, ok := strings.CutPrefix(strings.TrimSpace(line), "data: ")
	if !ok {
		t.Fatalf("line = %q, want a data: line", line)
	}
	var got device.StateChange
	if err := json.Unmarshal([]byte(data), &got); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	if got != change {
		t.Errorf("event = %+v, want %+v", got, change)
	}
}

func TestHubServeHTTPEndsOnClose(t *testing.T) {
	h := newTestHub()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	_, body := openStream(t, srv.URL)

	h.Close()

	done := make(chan error, 1)
	go func() {
		_, err := io.ReadAll(body)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("stream ended with error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stream still open 2s after Close")
	}
}
