package out

import "context"

type RoleRepository interface {
	GetPermissions(ctx context.Context, roleName string) ([]string, error)
}
