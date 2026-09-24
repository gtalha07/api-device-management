package device

import (
	"errors"
	"time"
)

type State string

const (
	StateAvailable State = "available"
	StateInUse     State = "in-use"
	StateInactive  State = "inactive"
)

// Valid reports whether s is one of the known device states
func (s State) Valid() bool {
	switch s {
	case StateAvailable, StateInUse, StateInactive:
		return true
	}

	return false
}

type Device struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Brand     string    `json:"brand"`
	State     State     `json:"state"`
	CreatedAt time.Time `json:"createdAt"`
}

var (
	ErrNotFound     = errors.New("device not found")
	ErrInUse        = errors.New("device is in use")
	ErrInvalidInput = errors.New("invalid input")
)
