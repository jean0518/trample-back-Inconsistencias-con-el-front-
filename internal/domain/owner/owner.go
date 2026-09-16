package owner

import "errors"

var ErrNotFound = errors.New("propietario no encontrado")

type Owner struct {
	ID        int64
	Name      string
	Phone     string
	Email     string
	IsDefault bool
}

type CreateInput struct {
	Name      string
	Phone     string
	Email     string
	IsDefault bool
}

type UpdateInput struct {
	Name  string
	Phone string
	Email string
}
