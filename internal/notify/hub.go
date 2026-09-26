package notify

import (
	"context"
	"log/slog"
	"sync"

	"github.com/gtalha07/api-device-management/internal/device"
)

// subscriberBuffer is how many events a subscriber can fall behind before
// new events are dropped for it.
const subscriberBuffer = 16

// Hub fans state changes out to clients subscribed over Server-Sent Events.
// A Subscriber/Client that falls behind the missed events doesn't get it
type Hub struct {
	logger *slog.Logger

	mu          sync.Mutex
	subscribers map[chan device.StateChange]struct{}
	closed      bool
	done        chan struct{}
}

// NewHub returns a Hub with no subscribers.
func NewHub(logger *slog.Logger) *Hub {
	return &Hub{
		logger:      logger,
		subscribers: make(map[chan device.StateChange]struct{}),
		done:        make(chan struct{}),
	}
}

var _ device.Notifier = (*Hub)(nil)

func (h *Hub) Notify(ctx context.Context, change device.StateChange) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	for ch := range h.subscribers {
		select {
		case ch <- change:
		default:
			h.logger.WarnContext(ctx, "dropped state change for slow subscriber", "device_id", change.DeviceID)
		}
	}
	return nil
}

func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.closed {
		// closing the channel twice panics in go.
		h.closed = true
		close(h.done)
	}
}

func (h *Hub) subscribe() (chan device.StateChange, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return nil, false
	}

	ch := make(chan device.StateChange, subscriberBuffer)
	h.subscribers[ch] = struct{}{}
	return ch, true
}

func (h *Hub) unsubscribe(ch chan device.StateChange) {
	h.mu.Lock()
	defer h.mu.Unlock()

	delete(h.subscribers, ch)
}
