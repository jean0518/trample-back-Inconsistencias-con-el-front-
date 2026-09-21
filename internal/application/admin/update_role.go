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
	staff out.AdminUserRepository
}

func NewUpdateRoleUseCase(staff out.AdminUserRepository) *UpdateRoleUseCase {
	return &UpdateRoleUseCase{staff: staff}
}

func (uc *UpdateRoleUseCase) Execute(ctx context.Context, in UpdateRoleInput) error {
	if _, ok := validAdminRoles[in.Role]; !ok {
		return ErrInvalidRole
	}

	perms, ok := DefaultPermissions(in.Role)
	if !ok {
		return ErrInvalidRole
	}

	return uc.staff.UpdateRole(ctx, in.UserID, in.Role, perms)
}