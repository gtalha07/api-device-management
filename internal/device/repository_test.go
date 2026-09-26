package device

import (
	"errors"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newTestRepo connects to TEST_DATABASE_URL and empties the devices table.
// Tests using it are skipped when the variable is unset.
func newTestRepo(t *testing.T) *PostgresRepository {
	t.Helper()

	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping Postgres integration test")
	}

	pool, err := pgxpool.New(t.Context(), url)
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(t.Context(), "TRUNCATE devices"); err != nil {
		t.Fatalf("truncate devices: %v", err)
	}

	return NewPostgresRepository(pool)
}

// mustCreate inserts device and fails the test on error.
func mustCreate(t *testing.T, repo *PostgresRepository, d Device) Device {
	t.Helper()

	created, err := repo.Create(t.Context(), d)
	if err != nil {
		t.Fatalf("Create %+v: %v", d, err)
	}
	return created
}

// ids returns the IDs of ds in order, for compact comparisons.
func ids(ds []Device) []string {
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = d.ID
	}
	return out
}

func TestPostgresRepositoryCreateAndGet(t *testing.T) {
	repo := newTestRepo(t)
	ctx := t.Context()

	created, err := repo.Create(ctx, Device{Name: "Phone X", Brand: "Acme", State: StateAvailable})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := uuid.Parse(created.ID); err != nil {
		t.Errorf("Create ID = %q, want a UUID", created.ID)
	}
	if created.CreatedAt.IsZero() || created.CreatedAt.Location() != time.UTC {
		t.Errorf("Create CreatedAt = %v, want a non-zero UTC time", created.CreatedAt)
	}

	got, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.CreatedAt.Equal(created.CreatedAt) {
		t.Errorf("Get CreatedAt = %v, want %v", got.CreatedAt, created.CreatedAt)
	}
	got.CreatedAt = created.CreatedAt
	if got != created {
		t.Errorf("Get = %+v, want %+v", got, created)
	}
}

func TestPostgresRepositoryGetNotFound(t *testing.T) {
	repo := newTestRepo(t)

	_, err := repo.Get(t.Context(), uuid.NewString())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get unknown id: err = %v, want ErrNotFound", err)
	}
}

func TestPostgresRepositoryList(t *testing.T) {
	repo := newTestRepo(t)

	a := mustCreate(t, repo, Device{Name: "Phone A", Brand: "Acme", State: StateAvailable})
	b := mustCreate(t, repo, Device{Name: "Tablet B", Brand: "Acme", State: StateInUse})
	c := mustCreate(t, repo, Device{Name: "Phone C", Brand: "Globex", State: StateAvailable})

	tests := []struct {
		name   string
		filter Filter
		want   []string
	}{
		{"no filter", Filter{}, []string{a.ID, b.ID, c.ID}},
		{"by brand", Filter{Brand: "Acme"}, []string{a.ID, b.ID}},
		{"by state", Filter{State: StateAvailable}, []string{a.ID, c.ID}},
		{"by brand and state", Filter{Brand: "Acme", State: StateAvailable}, []string{a.ID}},
		{"no match", Filter{Brand: "Initech"}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := repo.List(t.Context(), tt.filter)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if got == nil {
				t.Fatal("List returned nil, want a non-nil slice")
			}
			if !slices.Equal(ids(got), tt.want) {
				t.Errorf("List IDs = %v, want %v", ids(got), tt.want)
			}
		})
	}
}

func TestPostgresRepositoryUpdate(t *testing.T) {
	repo := newTestRepo(t)
	ctx := t.Context()
	d := mustCreate(t, repo, Device{Name: "Phone X", Brand: "Acme", State: StateAvailable})

	updated, err := repo.Update(ctx, d.ID, func(d *Device) error {
		d.Name = "Phone Y"
		d.State = StateInUse
		d.CreatedAt = time.Time{} // must not reach the database
		return nil
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := repo.Get(ctx, d.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != updated {
		t.Errorf("Get = %+v, want what Update returned %+v", got, updated)
	}
	if got.Name != "Phone Y" || got.State != StateInUse {
		t.Errorf("Get = %+v, want name %q and state %q", got, "Phone Y", StateInUse)
	}
	if !got.CreatedAt.Equal(d.CreatedAt) {
		t.Errorf("CreatedAt = %v, want unchanged %v", got.CreatedAt, d.CreatedAt)
	}
}

func TestPostgresRepositoryUpdateRollsBackOnError(t *testing.T) {
	repo := newTestRepo(t)
	ctx := t.Context()
	d := mustCreate(t, repo, Device{Name: "Phone X", Brand: "Acme", State: StateInUse})

	_, err := repo.Update(ctx, d.ID, func(d *Device) error {
		d.Name = "Phone Y"
		return ErrInUse
	})
	if !errors.Is(err, ErrInUse) {
		t.Fatalf("Update: err = %v, want ErrInUse", err)
	}

	got, err := repo.Get(ctx, d.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != d {
		t.Errorf("Get = %+v, want unchanged %+v", got, d)
	}
}

func TestPostgresRepositoryUpdateNotFound(t *testing.T) {
	repo := newTestRepo(t)

	called := false
	_, err := repo.Update(t.Context(), uuid.NewString(), func(*Device) error {
		called = true
		return nil
	})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Update unknown id: err = %v, want ErrNotFound", err)
	}
	if called {
		t.Error("fn was called for a missing device")
	}
}

// Two concurrent Updates race to claim the same available device. The row
// lock makes the second wait and see the first one's commit, so exactly one
// wins. Without FOR UPDATE both would read "available" and both succeed.
func TestPostgresRepositoryUpdateLocksRow(t *testing.T) {
	repo := newTestRepo(t)
	d := mustCreate(t, repo, Device{Name: "Phone X", Brand: "Acme", State: StateAvailable})

	claim := func(d *Device) error {
		if d.State == StateInUse {
			return ErrInUse
		}
		time.Sleep(50 * time.Millisecond) // hold the lock so the other call has to wait
		d.State = StateInUse
		return nil
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Go(func() {
			_, errs[i] = repo.Update(t.Context(), d.ID, claim)
		})
	}
	wg.Wait()

	var won, lost int
	for _, err := range errs {
		switch {
		case err == nil:
			won++
		case errors.Is(err, ErrInUse):
			lost++
		default:
			t.Fatalf("Update: unexpected error %v", err)
		}
	}
	if won != 1 || lost != 1 {
		t.Errorf("won = %d, lost = %d; want exactly one of each", won, lost)
	}
}

func TestPostgresRepositoryDelete(t *testing.T) {
	repo := newTestRepo(t)
	ctx := t.Context()
	d := mustCreate(t, repo, Device{Name: "Phone X", Brand: "Acme", State: StateAvailable})

	var checked Device
	err := repo.Delete(ctx, d.ID, func(d Device) error {
		checked = d
		return nil
	})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if checked != d {
		t.Errorf("check saw %+v, want %+v", checked, d)
	}

	if _, err := repo.Get(ctx, d.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Delete: err = %v, want ErrNotFound", err)
	}
}

func TestPostgresRepositoryDeleteKeepsRowOnCheckError(t *testing.T) {
	repo := newTestRepo(t)
	ctx := t.Context()
	d := mustCreate(t, repo, Device{Name: "Phone X", Brand: "Acme", State: StateInUse})

	err := repo.Delete(ctx, d.ID, func(Device) error { return ErrInUse })
	if !errors.Is(err, ErrInUse) {
		t.Fatalf("Delete: err = %v, want ErrInUse", err)
	}

	if _, err := repo.Get(ctx, d.ID); err != nil {
		t.Errorf("Get after refused Delete: %v, want the device to still exist", err)
	}
}

func TestPostgresRepositoryDeleteNotFound(t *testing.T) {
	repo := newTestRepo(t)

	err := repo.Delete(t.Context(), uuid.NewString(), func(Device) error { return nil })
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete unknown id: err = %v, want ErrNotFound", err)
	}
}
