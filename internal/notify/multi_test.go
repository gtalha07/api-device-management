package notify

import (
	"context"
	"errors"
	"testing"

	"github.com/gtalha07/api-device-management/internal/device"
)

type recordingNotifier struct {
	err     error
	changes []device.StateChange
}

func (r *recordingNotifier) Notify(_ context.Context, change device.StateChange) error {
	r.changes = append(r.changes, change)
	return r.err
}

func TestMultiNotifiesAllAndJoinsErrors(t *testing.T) {
	errFirst := errors.New("first failed")
	first := &recordingNotifier{err: errFirst}
	second := &recordingNotifier{}

	change := testChange("d1")
	err := Multi{first, second}.Notify(t.Context(), change)

	if !errors.Is(err, errFirst) {
		t.Errorf("err = %v, want it to wrap %v", err, errFirst)
	}
	for name, n := range map[string]*recordingNotifier{"first": first, "second": second} {
		if len(n.changes) != 1 || n.changes[0] != change {
			t.Errorf("%s notifier got %+v, want [%+v]", name, n.changes, change)
		}
	}
}
