package device

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Filter struct {
	Brand string
	State State
}

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

func (s *Service) List(ctx context.Context, f Filter) ([]Device, error) {
	f.Brand = strings.TrimSpace(f.Brand)

	if f.State != "" && !f.State.Valid() {
		return nil, fmt.Errorf("%w: unknown state %q", ErrInvalidInput, f.State)
	}

	return s.repo.List(ctx, f)
}

type UpdateInput struct {
	Name  *string
	Brand *string
	State *State
}

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
		err := s.notifier.Notify(ctx, change)
		if err != nil {
			s.logger.Error("notify state change failed", "device_id", updated.ID, "err", err)
		}
	}

	return updated, nil
}

// validateID rejects malformed ids as invalid input (400); passed to
// Postgres, they would fail the uuid column cast and surface as a 500.
func validateID(id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return fmt.Errorf("%w: invalid id %q", ErrInvalidInput, id)
	}
	return nil
}
