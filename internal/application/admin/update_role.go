package admin

import (
	"context"

	"trample-back/internal/ports/out"
)

type UpdateRoleInput struct {
	UserID int64
	Role   string
}

type UpdateRoleUseCase struct {
	users out.UserRepository
	roles out.RoleRepository
}

func NewUpdateRoleUseCase(users out.UserRepository, roles out.RoleRepository) *UpdateRoleUseCase {
	return &UpdateRoleUseCase{users: users, roles: roles}
}

func (uc *UpdateRoleUseCase) Execute(ctx context.Context, in UpdateRoleInput) error {
	if _, ok := validAdminRoles[in.Role]; !ok {
		return ErrInvalidRole
	}

	perms, err := uc.roles.GetPermissions(ctx, in.Role)
	if err != nil {
		return err
	}

	return uc.users.UpdateRole(ctx, in.UserID, in.Role, perms)
}
