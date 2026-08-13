package auth

import "errors"

var ErrEmailTaken = errors.New("email ya registrado")

const (
	RoleCustomer = "customer"
	RoleAdmin    = "admin"
)

type User struct {
	ID        int64
	FirstName string
	LastName  string
	Email     string
	Password  string // bcrypt hash
	Role      string
}
