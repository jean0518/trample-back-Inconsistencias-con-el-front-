package admin

import (
	"context"
	"trample-back/internal/domain/auth"
	"trample-back/internal/ports/out"
)

type ListAdminsUseCase struct {
	staff out.AdminUserRepository
}

func NewListAdminsUseCase(staff out.AdminUserRepository) *ListAdminsUseCase {
	return &ListAdminsUseCase{staff: staff}
}

func (uc *ListAdminsUseCase) Execute(ctx context.Context) ([]auth.User, error) {
	return uc.staff.List(ctx)
}