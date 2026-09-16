package admin

import (
	"context"
	"trample-back/internal/ports/out"
)

type DeleteAdminUseCase struct {
	staff out.AdminUserRepository
}

func NewDeleteAdminUseCase(staff out.AdminUserRepository) *DeleteAdminUseCase {
	return &DeleteAdminUseCase{staff: staff}
}

func (uc *DeleteAdminUseCase) Execute(ctx context.Context, id int64) error {
	return uc.staff.Delete(ctx, id)
}