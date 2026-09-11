package admin

import (
	"context"
	"trample-back/internal/ports/out"
)

type DeleteAdminUseCase struct {
	users out.UserRepository
}

func NewDeleteAdminUseCase(users out.UserRepository) *DeleteAdminUseCase {
	return &DeleteAdminUseCase{users: users}
}

func (uc *DeleteAdminUseCase) Execute(ctx context.Context, id int64) error {
	return uc.users.Delete(ctx, id)
}
