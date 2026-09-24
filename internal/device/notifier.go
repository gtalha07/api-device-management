package device

import (
	"context"
	"time"
)

type StateChange struct {
	DeviceID  string    `json:"deviceId"`
	Previous  State     `json:"previous"`
	Current   State     `json:"current"`
	ChangedAt time.Time `json:"changedAt"` // subscribers always want to know when
}

type Notifier interface {
	Notify(ctx context.Context, change StateChange) error
}
