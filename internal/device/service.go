package device

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Filter narrows List results. Empty fields don't filter.
//
// TODO: brand matches exactly and case-sensitively; case-insensitive search
// would need lower(brand) in the query and an index on lower(brand).
type Filter struct {
	Brand string
	State State
}

// Repository stores devices. Update and Delete run their callback while the
// device is locked, so the callback sees the current row and no other write
// can happen between its check and the change. A callback error aborts the
// operation, nothing is written, and the error is returned unchanged.
type Repository interface {
	Create(ctx context.Context, d Device) (Device, error)
	Get(ctx context.Context, id string) (Device, error)
	List(ctx context.Context, filter Filter) ([]Device, error)
	Update(ctx context.Context, id string, fn func(d *Device) error) (Device, error)
	Delete(ctx context.Context, id string, check func(d Device) error) error
}

type Service struct {
	repo     Repository
	notifier Notifier
	logger   *slog.Logger
}

// NewService returns a Service. notifier must not be nil: it is called on
// every state change.
func NewService(repo Repository, notifier Notifier, logger *slog.Logger) *Service {
	return &Service{repo: repo, notifier: notifier, logger: logger}
}

type CreateInput struct {
	Name  string
	Brand string
	State State
}

func (s *Service) Create(ctx context.Context, in CreateInput) (Device, error) {
	name := strings.TrimSpace(in.Name)
	brand := strings.TrimSpace(in.Brand)

	if name == "" {
		return Device{}, fmt.Errorf("%w: name is required", ErrInvalidInput)
	}

	if brand == "" {
		return Device{}, fmt.Errorf("%w: brand is required", ErrInvalidInput)
	}

	if in.State == "" {
		in.State = StateAvailable
	}

	if !in.State.Valid() {
		return Device{}, fmt.Errorf("%w: unknown state %q", ErrInvalidInput, in.State)
	}

	device := Device{
		// ID and CreatedAt are left as is, database generates them.
		// so every app instance uses same clock and id generator
		Name:  name,
		Brand: brand,
		State: in.State,
	}

	return s.repo.Create(ctx, device)
}

func (s *Service) Get(ctx context.Context, id string) (Device, error) {
	// check format for database
	if err := validateID(id); err != nil {
		return Device{}, err
	}

	return s.repo.Get(ctx, id)
}

// List returns the devices matching f.
//
// TODO: no pagination; every matching row is returned. Keyset pagination on
// (created_at, id), which List already orders by, would bound the response.
func (s *Service) List(ctx context.Context, f Filter) ([]Device, error) {
	f.Brand = strings.TrimSpace(f.Brand)

	if f.State != "" && !f.State.Valid() {
		return nil, fmt.Errorf("%w: unknown state %q", ErrInvalidInput, f.State)
	}

	return s.repo.List(ctx, f)
}

// UpdateInput holds the fields to change. A nil field is left unchanged.
type UpdateInput struct {
	Name  *string
	Brand *string
	State *State
}

// Update applies in to the device with the given id. Name and brand can't
// change while the device is in use. Subscribers are notified only after a
// state change has been committed.
func (s *Service) Update(ctx context.Context, id string, in UpdateInput) (Device, error) {
	// 1. Validate id
	if err := validateID(id); err != nil {
		return Device{}, err
	}

	var prev State
	updated, err := s.repo.Update(ctx, id, func(d *Device) error {
		prev = d.State
		// 2. apply and validate changes
		if in.Name != nil {
			name := strings.TrimSpace(*in.Name)
			if name == "" {
				return fmt.Errorf("%w: invalid name", ErrInvalidInput)
			}
			if prev == StateInUse && name != d.Name {
				return fmt.Errorf("%w: cannot change name", ErrInUse)
			}
			d.Name = name
		}

		if in.Brand != nil {
			brand := strings.TrimSpace(*in.Brand)
			if brand == "" {
				return fmt.Errorf("%w: invalid brand", ErrInvalidInput)
			}
			if prev == StateInUse && brand != d.Brand {
				return fmt.Errorf("%w: cannot change brand", ErrInUse)
			}
			d.Brand = brand
		}

		if in.State != nil {
			if !in.State.Valid() {
				return fmt.Errorf("%w: unknown state %q", ErrInvalidInput, *in.State)
			}
			d.State = *in.State
		}

		return nil
	})

	if err != nil {
		return Device{}, err
	}

	// 3. notify if state changes
	if prev != updated.State {
		change := StateChange{
			DeviceID:  updated.ID,
			Previous:  prev,
			Current:   updated.State,
			ChangedAt: time.Now().UTC(),
		}
		// TODO: ctx is the request context, so a client disconnecting right
		// after the commit would cancel a network-based delivery. Pass
		// context.WithoutCancel(ctx) once Notify does real I/O.
		err := s.notifier.Notify(ctx, change)
		if err != nil {
			s.logger.ErrorContext(ctx, "notify state change failed", "device_id", updated.ID, "err", err)
		}
	}

	return updated, nil
}

func (s *Service) Delete(ctx context.Context, id string) error {
	if err := validateID(id); err != nil {
		return err
	}

	return s.repo.Delete(ctx, id, func(d Device) error {
		if d.State == StateInUse {
			return fmt.Errorf("%w: cannot delete device", ErrInUse)
		}

		return nil
	})
}

// validateID rejects malformed ids as invalid input (400); passed to
// Postgres, they would fail the uuid column cast and surface as a 500.
func validateID(id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return fmt.Errorf("%w: invalid id %q", ErrInvalidInput, id)
	}
	return nil
}
