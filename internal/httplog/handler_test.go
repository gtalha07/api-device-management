package httplog

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestHandlerAddsRequestID(t *testing.T) {
	ctx := context.WithValue(t.Context(), ctxKey{}, "req-1")

	tests := []struct {
		name string
		log  func(l *slog.Logger)
		want string
	}{
		{"context with id", func(l *slog.Logger) { l.InfoContext(ctx, "hi") }, "req-1"},
		{"context without id", func(l *slog.Logger) { l.InfoContext(t.Context(), "hi") }, ""},
		{"logger built with With", func(l *slog.Logger) { l.With("k", "v").InfoContext(ctx, "hi") }, "req-1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			tt.log(slog.New(NewHandler(slog.NewJSONHandler(&buf, nil))))

			var got struct {
				RequestID string `json:"request_id"`
			}
			if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
				t.Fatalf("log line is not JSON: %v\n%s", err, buf.String())
			}
			if got.RequestID != tt.want {
				t.Errorf("request_id = %q, want %q", got.RequestID, tt.want)
			}
		})
	}
}

func TestHandlerWithGroupKeepsWrapper(t *testing.T) {
	if _, ok := NewHandler(slog.DiscardHandler).WithGroup("g").(*Handler); !ok {
		t.Error("WithGroup returned the inner handler; request ids would be lost")
	}
}
