package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gtalha07/api-device-management/internal/device"
)

// subscriberBuffer is how many events a subscriber can fall behind before
// new events are dropped for it.
const subscriberBuffer = 16

// keepAliveInterval is how often an idle stream gets a comment line, so
// proxies and load balancers don't close it as idle.
const keepAliveInterval = 15 * time.Second

// Hub fans state changes out to clients subscribed over Server-Sent Events.
// Delivery is best effort: a subscriber that falls behind misses events, and
// a client that reconnects doesn't get the ones it missed.
//
// TODO: the hub is in memory, so each instance only sees its own changes. With
// several replicas, fan out through a shared broker (Postgres LISTEN/NOTIFY,
// Redis, NATS); add event ids so clients can resume with Last-Event-ID.
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

// ServeHTTP streams state changes to the client as Server-Sent Events until
// the client disconnects or the Hub is closed.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ch, ok := h.subscribe()
	if !ok {
		http.Error(w, "shutting down", http.StatusServiceUnavailable)
		return
	}

	defer h.unsubscribe(ch)

	rc := http.NewResponseController(w)
	// The server's WriteTimeout would cut a long-lived stream; lift it here.
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		h.logger.ErrorContext(r.Context(), "clear write deadline", "err", err)
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	// sends the headers right away
	if err := rc.Flush(); err != nil {
		return
	}

	keepAlive := time.NewTicker(keepAliveInterval)
	defer keepAlive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-h.done:
			return
		case change := <-ch:
			data, err := json.Marshal(change)
			if err != nil {
				h.logger.ErrorContext(r.Context(), "state change", "err", err)
				continue
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return
			}
		case <-keepAlive.C:
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				return
			}
		}

		if err := rc.Flush(); err != nil {
			return
		}
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
