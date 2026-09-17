package auth

import "errors"

var ErrEmailTaken = errors.New("email ya registrado")
var ErrUserNotFound = errors.New("usuario no encontrado")
var ErrIsAdmin = errors.New("no se puede modificar a un administrador")

const (
	RoleCustomer       = "customer"
	RoleAdmin          = "admin"
	RoleColaborador    = "colaborador"
	RoleSupColaborador = "sup_colaborador"
	RolePersonalizado  = "personalizado"
)

// IsStaffRole indica si un rol da acceso al panel administrativo.
// Es cualquier rol distinto de "customer"; los paneles asignados deciden
// qué secciones puede ver.
func IsStaffRole(role string) bool {
	return role != RoleCustomer && role != ""
}

const (
	PermResumen      = "resumen"
	PermVentas       = "ventas"
	PermPedidos      = "pedidos"
	PermInventario   = "inventario"
	PermPropietarios = "propietarios"
	PermReservas     = "reservas"
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
