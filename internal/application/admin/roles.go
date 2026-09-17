package admin

import (
	"sort"

	"trample-back/internal/domain/auth"
)

// rolePresets define la equivalencia rol -> paneles por defecto.
// El rol es únicamente una etiqueta descriptiva: al elegirlo en el front se
// activan estos paneles en los selects. No tiene relación en la base de datos.
var rolePresets = map[string][]string{
	auth.RoleColaborador: {
		auth.PermPedidos, auth.PermInventario, auth.PermReservas, auth.PermPropietarios, auth.PermResumen,
	},
	auth.RoleSupColaborador: {
		auth.PermPedidos, auth.PermInventario, auth.PermReservas,
	},
}

// validAdminRoles son los roles que se pueden asignar al crear o cambiar rol
// de un usuario del panel (los admins existen, pero no se gestionan aquí).
var validAdminRoles = map[string]struct{}{
	auth.RoleColaborador:    {},
	auth.RoleSupColaborador: {},
	auth.RolePersonalizado:  {},
}

func samePermissionSet(a, b []string) bool {
	sa := append([]string{}, a...)
	sb := append([]string{}, b...)
	sort.Strings(sa)
	sort.Strings(sb)
	if len(sa) != len(sb) {
		return false
	}
	for i := range sa {
		if sa[i] != sb[i] {
			return false
		}
	}
	return true
}

// DefaultPermissions devuelve los paneles por defecto de un rol preseteado.
func DefaultPermissions(role string) ([]string, bool) {
	perms, ok := rolePresets[role]
	if !ok {
		return nil, false
	}
	return append([]string{}, perms...), true
}

// ResolveRoleLabel decide la etiqueta del rol a partir de los paneles
// seleccionados. Si los permisos coinciden con el preset de algún rol, se usa
// ese rol; si no, el rol es "personalizado" (solo descripción).
func ResolveRoleLabel(requestedRole string, permissions []string) string {
	if preset, ok := rolePresets[requestedRole]; ok && samePermissionSet(preset, permissions) {
		return requestedRole
	}
	for role, preset := range rolePresets {
		if samePermissionSet(preset, permissions) {
			return role
		}
	}
	return auth.RolePersonalizado
}