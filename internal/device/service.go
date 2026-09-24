package device

import "context"

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
