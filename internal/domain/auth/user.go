package auth

import "errors"

var ErrEmailTaken = errors.New("email ya registrado")
var ErrUserNotFound = errors.New("usuario no encontrado")

const (
	RoleCustomer       = "customer"
	RoleAdmin          = "admin"
	RoleColaborador    = "colaborador"
	RoleSupColaborador = "sup_colaborador"
)

const (
	PermVentas       = "ventas"
	PermInventario   = "inventario"
	PermPropietarios = "propietarios"
	PermCatalogo     = "catalogo"
)

type User struct {
	ID          int64
	FirstName   string
	LastName    string
	Email       string
	Password    string // bcrypt hash
	Role        string
	Permissions []string
}
