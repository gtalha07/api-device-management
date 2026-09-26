package notify

import (
	"io"
	"log/slog"
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
