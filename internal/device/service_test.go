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

	created   []Device // every Device passed to Create
	getIDs    []string // every id passed to Get
	filters   []Filter // every Filter passed to List
	updateIDs []string // every id passed to Update
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

// Update mimics the real repository's contract: fn works on a copy, and the
// copy is stored only if fn succeeds, so a failing fn behaves like a rollback.
func (f *fakeRepo) Update(ctx context.Context, id string, fn func(d *Device) error) (Device, error) {
	f.updateIDs = append(f.updateIDs, id)
	d, ok := f.devices[id]
	if !ok {
		return Device{}, ErrNotFound
	}
	if err := fn(&d); err != nil {
		return Device{}, err
	}
	f.devices[id] = d
	return d, nil
}

type fakeNotifier struct {
	err     error         // returned from every Notify call
	changes []StateChange // every change passed to Notify
}

func (n *fakeNotifier) Notify(ctx context.Context, change StateChange) error {
	n.changes = append(n.changes, change)
	return n.err
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestService(repo Repository) *Service {
	return NewService(repo, nil, discardLogger())
}

func ptr[T any](v T) *T { return &v }

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

func TestServiceUpdate(t *testing.T) {
	const id = "7f1c2b9e-3a4d-4e5f-8a6b-1c2d3e4f5a6b"
	const unknownID = "00000000-0000-4000-8000-000000000000"
	createdAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	stored := func(state State) Device {
		return Device{ID: id, Name: "Phone", Brand: "Acme", State: state, CreatedAt: createdAt}
	}

	tests := []struct {
		name       string
		id         string
		start      State // stored state before the request
		in         UpdateInput
		notifyErr  error
		wantErr    error
		want       Device       // stored device afterwards (unchanged on error)
		wantChange *StateChange // expected notification, nil = none
	}{
		{
			name:  "partial: rename available device",
			id:    id,
			start: StateAvailable,
			in:    UpdateInput{Name: ptr("Tablet")},
			want:  Device{ID: id, Name: "Tablet", Brand: "Acme", State: StateAvailable, CreatedAt: createdAt},
		},
		{
			name:  "name and brand are trimmed",
			id:    id,
			start: StateAvailable,
			in:    UpdateInput{Name: ptr("  Tablet "), Brand: ptr("\tGlobex\n")},
			want:  Device{ID: id, Name: "Tablet", Brand: "Globex", State: StateAvailable, CreatedAt: createdAt},
		},
		{
			name:       "state change notifies with previous and current",
			id:         id,
			start:      StateAvailable,
			in:         UpdateInput{State: ptr(StateInUse)},
			want:       stored(StateInUse),
			wantChange: &StateChange{DeviceID: id, Previous: StateAvailable, Current: StateInUse},
		},
		{
			name:  "same state does not notify",
			id:    id,
			start: StateInUse,
			in:    UpdateInput{State: ptr(StateInUse)},
			want:  stored(StateInUse),
		},
		{
			name:  "full update with unchanged name and brand allowed while in use",
			id:    id,
			start: StateInUse,
			in:    UpdateInput{Name: ptr("Phone"), Brand: ptr("Acme"), State: ptr(StateInUse)},
			want:  stored(StateInUse),
		},
		{
			name:       "releasing an in-use device is allowed",
			id:         id,
			start:      StateInUse,
			in:         UpdateInput{State: ptr(StateAvailable)},
			want:       stored(StateAvailable),
			wantChange: &StateChange{DeviceID: id, Previous: StateInUse, Current: StateAvailable},
		},
		{
			name:    "rename while in use",
			id:      id,
			start:   StateInUse,
			in:      UpdateInput{Name: ptr("Tablet")},
			wantErr: ErrInUse,
			want:    stored(StateInUse),
		},
		{
			name:    "rebrand while in use",
			id:      id,
			start:   StateInUse,
			in:      UpdateInput{Brand: ptr("Globex")},
			wantErr: ErrInUse,
			want:    stored(StateInUse),
		},
		{
			name:    "rename and release in one request uses stored state",
			id:      id,
			start:   StateInUse,
			in:      UpdateInput{Name: ptr("Tablet"), State: ptr(StateAvailable)},
			wantErr: ErrInUse,
			want:    stored(StateInUse),
		},
		{
			name:    "empty name",
			id:      id,
			start:   StateAvailable,
			in:      UpdateInput{Name: ptr("   ")},
			wantErr: ErrInvalidInput,
			want:    stored(StateAvailable),
		},
		{
			name:    "unknown state is rejected, not ignored",
			id:      id,
			start:   StateAvailable,
			in:      UpdateInput{State: ptr(State("banana"))},
			wantErr: ErrInvalidInput,
			want:    stored(StateAvailable),
		},
		{
			name:    "unknown id",
			id:      unknownID,
			start:   StateAvailable,
			in:      UpdateInput{Name: ptr("Tablet")},
			wantErr: ErrNotFound,
			want:    stored(StateAvailable),
		},
		{
			name:       "notify failure still returns success",
			id:         id,
			start:      StateAvailable,
			in:         UpdateInput{State: ptr(StateInactive)},
			notifyErr:  errors.New("subscriber down"),
			want:       stored(StateInactive),
			wantChange: &StateChange{DeviceID: id, Previous: StateAvailable, Current: StateInactive},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{devices: map[string]Device{id: stored(tt.start)}}
			notifier := &fakeNotifier{err: tt.notifyErr}
			svc := NewService(repo, notifier, discardLogger())

			got, err := svc.Update(context.Background(), tt.id, tt.in)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got != tt.want {
					t.Errorf("returned %+v, want %+v", got, tt.want)
				}
			}

			if saved := repo.devices[id]; saved != tt.want {
				t.Errorf("stored %+v, want %+v", saved, tt.want)
			}

			if tt.wantChange == nil {
				if len(notifier.changes) != 0 {
					t.Fatalf("unexpected notifications: %+v", notifier.changes)
				}
				return
			}
			if len(notifier.changes) != 1 {
				t.Fatalf("got %d notifications, want 1", len(notifier.changes))
			}
			c := notifier.changes[0]
			if c.DeviceID != tt.wantChange.DeviceID || c.Previous != tt.wantChange.Previous || c.Current != tt.wantChange.Current {
				t.Errorf("notified %+v, want %+v", c, *tt.wantChange)
			}
			if c.ChangedAt.IsZero() {
				t.Error("notification missing ChangedAt")
			}
		})
	}
}

func TestServiceUpdateMalformedIDSkipsRepo(t *testing.T) {
	repo := &fakeRepo{devices: map[string]Device{}}
	svc := NewService(repo, &fakeNotifier{}, discardLogger())

	_, err := svc.Update(context.Background(), "abc", UpdateInput{Name: ptr("Tablet")})

	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err = %v, want %v", err, ErrInvalidInput)
	}
	if len(repo.updateIDs) != 0 {
		t.Fatalf("repo.Update was called with %v", repo.updateIDs)
	}
}
