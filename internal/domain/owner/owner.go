package owner

import (
	"errors"
	"time"
)

var ErrNotFound = errors.New("propietario no encontrado")

type Owner struct {
	ID        int64
	Name      string
	Phone     string
	Email     string
	IsDefault bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type CreateInput struct {
	Name      string
	Phone     string
	Email     string
	IsDefault bool
}
