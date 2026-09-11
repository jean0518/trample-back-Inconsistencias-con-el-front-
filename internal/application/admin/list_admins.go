package admin

import (
	"context"
	"trample-back/internal/domain/auth"
	"trample-back/internal/ports/out"
)

type ListAdminsUseCase struct {
	users out.UserRepository
}

func NewListAdminsUseCase(users out.UserRepository) *ListAdminsUseCase {
	return &ListAdminsUseCase{users: users}
}

func (uc *ListAdminsUseCase) Execute(ctx context.Context) ([]auth.User, error) {
	return uc.users.ListAdmins(ctx)
}
