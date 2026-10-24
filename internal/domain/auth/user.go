package auth

import (
	"errors"
	"time"
)

var ErrEmailTaken = errors.New("email ya registrado")
var ErrUserNotFound = errors.New("usuario no encontrado")
var ErrIsAdmin = errors.New("no se puede modificar a un administrador")
var ErrInvalidVerifyToken = errors.New("el enlace de verificación es inválido o ya expiró")
var ErrUserInactive = errors.New("usuario desactivado")

// VerifyTokenTTL es lo que dura el enlace de verificación del correo.
const VerifyTokenTTL = 24 * time.Hour

const (
	RoleCustomer       = "customer"
	RoleAdmin          = "admin"
	RoleColaborador    = "colaborador"
	RoleSupColaborador = "sup_colaborador"
	RolePersonalizado  = "personalizado"
)

// Estado del staff en admin_users. Desactivar no borra: conserva el historial
// (ventas, listings) y solo bloquea el acceso al panel.
const (
	StatusActive   = "active"
	StatusInactive = "inactive"
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
	PermJuegos       = "juegos"
	PermPropietarios = "propietarios"
	PermReservas     = "reservas"
)

type User struct {
	ID          string
	FirstName   string
	LastName    string
	Email       string
	Password    string // bcrypt hash
	Role        string
	Permissions []string

	// Status del staff en admin_users ("active" | "inactive"). Los clientes
	// no tienen registro ahí; para ellos queda vacío y se consideran activos.
	Status string

	// IsDirectAdmin identifica a los administradores originales, no a los
	// usuarios admin creados o editables desde el panel (created_by no nulo).
	IsDirectAdmin bool

	// Verificación del correo. El token solo existe mientras hay una
	// verificación pendiente.
	EmailVerified        bool
	EmailVerifyToken     string
	EmailVerifyExpiresAt time.Time
}

// IsActive indica si el usuario puede acceder. Un Status vacío (cliente sin
// registro de staff) se considera activo.
func (u User) IsActive() bool {
	return u.Status != StatusInactive
}
