package notify

import (
	"context"
	"errors"

	"github.com/gtalha07/api-device-management/internal/device"
)

// Multi sends each change to every notifier in order. One failing notifier
// doesn't stop the others; their errors are joined.
type Multi []device.Notifier

var _ device.Notifier = Multi(nil)

// Notify calls Notify on every notifier in m.
func (m Multi) Notify(ctx context.Context, change device.StateChange) error {
	var errs []error

	for _, n := range m {
		if err := n.Notify(ctx, change); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
