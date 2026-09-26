package device

import (
	"context"
	"time"
)

// StateChange is the event subscribers receive after a device's state change
// has been committed.
type StateChange struct {
	DeviceID  string    `json:"deviceId"`
	Previous  State     `json:"previous"`
	Current   State     `json:"current"`
	ChangedAt time.Time `json:"changedAt"` // subscribers always want to know when
}

// Notifier delivers state changes to subscribers. The service calls it only
// after the change is committed; a failed delivery is logged, never returned
// to the client, because the change itself has already succeeded.
//
// TODO: delivery is at-most-once: a crash between commit and Notify loses the
// event. A transactional outbox (write the event in the same transaction,
// publish it from a background worker) would make it at-least-once.
type Notifier interface {
	Notify(ctx context.Context, change StateChange) error
}
