package out

import (
	"context"
	"trample-back/internal/domain/auth"
)

// AdminUserRepository gestiona el staff (usuarios creados por el admin).
// Los clientes viven únicamente en users; el staff vive en admin_users y
// sus paneles de acceso en admin_user_panels.
type AdminUserRepository interface {
	// CreateStaff crea el usuario base y su registro de staff + paneles.
	CreateStaff(ctx context.Context, user auth.User, createdBy int64) (auth.User, error)
	// List devuelve todo el staff del panel (admin, colaborador, sup_colaborador, personalizado).
	List(ctx context.Context) ([]auth.User, error)
	// UpdateRole cambia el rol del staff y reasigna los paneles por defecto.
	UpdateRole(ctx context.Context, userID int64, role string, permissions []string) error
	// UpdatePanels reemplaza los paneles asignados y ajusta la etiqueta del rol.
	UpdatePanels(ctx context.Context, userID int64, panels []string, role string) error
	// UpdateProfile actualiza los datos del staff (nombre, correo, contraseña
	// opcional), la etiqueta de rol y los paneles asignados.
	UpdateProfile(ctx context.Context, userID int64, user auth.User, role string) error
	// Delete elimina un usuario de staff (nunca a un admin) junto con su registro en users.
	Delete(ctx context.Context, userID int64) error
}