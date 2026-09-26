// Package notify delivers device state changes to subscribers.
package notify

import (
	"context"
	"log/slog"

	"github.com/gtalha07/api-device-management/internal/device"
)

// LogNotifier publishes state changes as structured log events. It stands in
// for a real transport (webhook, message broker) behind the same interface.
//
// TODO: replace with a real transport; only this type and main need to change.
type LogNotifier struct {
	logger *slog.Logger
}

func NewLogNotifier(logger *slog.Logger) *LogNotifier {
	return &LogNotifier{logger: logger}
}

var _ device.Notifier = (*LogNotifier)(nil)

func (n *LogNotifier) Notify(ctx context.Context, change device.StateChange) error {
	n.logger.InfoContext(ctx, "device state changed",
		"device_id", change.DeviceID,
		"previous", change.Previous,
		"current", change.Current,
		"changed_at", change.ChangedAt,
	)

	return nil
}
