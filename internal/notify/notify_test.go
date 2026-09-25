package notify

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/gtalha07/api-device-management/internal/device"
)

func TestLogNotifierNotify(t *testing.T) {
	var buf bytes.Buffer
	n := NewLogNotifier(slog.New(slog.NewJSONHandler(&buf, nil)))

	change := device.StateChange{
		DeviceID:  "3f2a9c1e-5b7d-4e8a-9c21-7d4e5f6a8b90",
		Previous:  device.StateAvailable,
		Current:   device.StateInUse,
		ChangedAt: time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC),
	}
	if err := n.Notify(t.Context(), change); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("log line is not JSON: %v\n%s", err, buf.String())
	}

	want := map[string]string{
		"msg":        "device state changed",
		"device_id":  change.DeviceID,
		"previous":   "available",
		"current":    "in-use",
		"changed_at": "2026-09-25T12:00:00Z",
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("%s = %v, want %q", key, got[key], value)
		}
	}
}
