package device

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

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
	if _, err := uuid.Parse(id); err != nil {
		// check the format for database
		return Device{}, fmt.Errorf("%w: invalid id %q", ErrInvalidInput, id)
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
