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

	devices map[string]Device // what Get serves
	listed  []Device          // what List returns

	created []Device // every Device passed to Create
	getIDs  []string // every id passed to Get
	filters []Filter // every Filter passed to List
}

func (f *fakeRepo) Create(ctx context.Context, d Device) (Device, error) {
	d.ID = "test-1"
	d.CreatedAt = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f.created = append(f.created, d)
	return d, nil
}

func (f *fakeRepo) Get(ctx context.Context, id string) (Device, error) {
	f.getIDs = append(f.getIDs, id)
	d, ok := f.devices[id]
	if !ok {
		return Device{}, ErrNotFound
	}
	return d, nil
}

func (f *fakeRepo) List(ctx context.Context, filter Filter) ([]Device, error) {
	f.filters = append(f.filters, filter)
	return f.listed, nil
}

func newTestService(repo Repository) *Service {
	return NewService(repo, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
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
			svc := newTestService(repo)

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

func TestServiceGet(t *testing.T) {
	const knownID = "7f1c2b9e-3a4d-4e5f-8a6b-1c2d3e4f5a6b"
	const unknownID = "00000000-0000-4000-8000-000000000000"
	known := Device{ID: knownID, Name: "Phone", Brand: "Acme", State: StateAvailable}

	tests := []struct {
		name        string
		id          string
		wantErr     error
		wantRepoHit bool // whether the call should reach the repository
	}{
		{name: "known id", id: knownID, wantRepoHit: true},
		{name: "unknown id passes repo ErrNotFound through", id: unknownID, wantErr: ErrNotFound, wantRepoHit: true},
		{name: "malformed id", id: "abc", wantErr: ErrInvalidInput},
		{name: "empty id", id: "", wantErr: ErrInvalidInput},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{devices: map[string]Device{knownID: known}}
			svc := newTestService(repo)

			got, err := svc.Get(context.Background(), tt.id)

			if hit := len(repo.getIDs) > 0; hit != tt.wantRepoHit {
				t.Fatalf("repo.Get called = %v, want %v", hit, tt.wantRepoHit)
			}
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != known {
				t.Errorf("got %+v, want %+v", got, known)
			}
		})
	}
}

func TestServiceList(t *testing.T) {
	tests := []struct {
		name       string
		filter     Filter
		wantErr    error
		wantFilter Filter // what the repository should receive
	}{
		{name: "no filter", filter: Filter{}, wantFilter: Filter{}},
		{name: "brand is trimmed", filter: Filter{Brand: "  Acme "}, wantFilter: Filter{Brand: "Acme"}},
		{name: "whitespace-only brand means no filter", filter: Filter{Brand: "   "}, wantFilter: Filter{}},
		{name: "valid state", filter: Filter{State: StateInUse}, wantFilter: Filter{State: StateInUse}},
		{
			name:       "brand and state together",
			filter:     Filter{Brand: "Acme", State: StateInactive},
			wantFilter: Filter{Brand: "Acme", State: StateInactive},
		},
		{name: "unknown state", filter: Filter{State: "banana"}, wantErr: ErrInvalidInput},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listed := []Device{{ID: "test-1", Name: "Phone", Brand: "Acme", State: StateInUse}}
			repo := &fakeRepo{listed: listed}
			svc := newTestService(repo)

			got, err := svc.List(context.Background(), tt.filter)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				if len(repo.filters) != 0 {
					t.Fatalf("repo.List was called on invalid input with %+v", repo.filters)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(repo.filters) != 1 {
				t.Fatalf("repo.List called %d times, want 1", len(repo.filters))
			}
			if repo.filters[0] != tt.wantFilter {
				t.Errorf("repo got filter %+v, want %+v", repo.filters[0], tt.wantFilter)
			}
			if len(got) != len(listed) {
				t.Errorf("got %d devices, want %d", len(got), len(listed))
			}
		})
	}
}
