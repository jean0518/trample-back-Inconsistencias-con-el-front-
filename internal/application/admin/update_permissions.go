package admin

import (
	"context"
	"trample-back/internal/ports/out"
)

type UpdatePermissionsInput struct {
	UserID      int64
	Permissions []string
}

type UpdatePermissionsUseCase struct {
	users out.UserRepository
}

func NewUpdatePermissionsUseCase(users out.UserRepository) *UpdatePermissionsUseCase {
	return &UpdatePermissionsUseCase{users: users}
}

func (uc *UpdatePermissionsUseCase) Execute(ctx context.Context, in UpdatePermissionsInput) error {
	for _, p := range in.Permissions {
		if _, ok := validPermissions[p]; !ok {
			return ErrInvalidPermission
		}
	}
	perms := in.Permissions
	if perms == nil {
		perms = []string{}
	}
	return uc.users.UpdatePermissions(ctx, in.UserID, perms)
}
