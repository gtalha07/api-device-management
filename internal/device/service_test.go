package device

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

type fakeRepo struct {
	Repository // embedded: satisfies the interface, unimplemented methods panic
	created    []Device
}

func (f *fakeRepo) Create(ctx context.Context, d Device) (Device, error) {
	d.ID = "test-1"
	d.CreatedAt = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f.created = append(f.created, d)
	return d, nil
}

func TestServiceCreate(t *testing.T) {
	tests := []struct {
		name    string
		in      CreateInput
		wantErr error
		want    Device // checked only when wantErr is nil
	}{
		{
			name: "valid",
			in:   CreateInput{Name: "Phone", Brand: "Acme", State: StateInUse},
			want: Device{Name: "Phone", Brand: "Acme", State: StateInUse},
		},
		{
			name: "empty state defaults to available",
			in:   CreateInput{Name: "Phone", Brand: "Acme"},
			want: Device{Name: "Phone", Brand: "Acme", State: StateAvailable},
		},
		{
			name: "name and brand are trimmed",
			in:   CreateInput{Name: "  Phone  ", Brand: "\tAcme\n"},
			want: Device{Name: "Phone", Brand: "Acme", State: StateAvailable},
		},
		{
			name:    "empty name",
			in:      CreateInput{Brand: "Acme"},
			wantErr: ErrInvalidInput,
		},
		{
			name:    "whitespace-only brand",
			in:      CreateInput{Name: "Phone", Brand: "   "},
			wantErr: ErrInvalidInput,
		},
		{
			name:    "unknown state",
			in:      CreateInput{Name: "Phone", Brand: "Acme", State: "broken"},
			wantErr: ErrInvalidInput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{}
			svc := NewService(repo, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

			got, err := svc.Create(context.Background(), tt.in)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				if len(repo.created) != 0 {
					t.Fatalf("repo.Create was called on invalid input: %+v", repo.created)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(repo.created) != 1 {
				t.Fatalf("repo.Create called %d times, want 1", len(repo.created))
			}

			saved := repo.created[0]
			if saved.Name != tt.want.Name || saved.Brand != tt.want.Brand || saved.State != tt.want.State {
				t.Errorf("saved = %+v, want name=%q brand=%q state=%q",
					saved, tt.want.Name, tt.want.Brand, tt.want.State)
			}
			if got.ID == "" || got.CreatedAt.IsZero() {
				t.Errorf("returned device missing DB-generated fields: %+v", got)
			}
		})
	}
}
