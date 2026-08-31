package owner

import (
	"context"

	"trample-back/internal/domain/owner"
	"trample-back/internal/ports/out"
)

type ListOwnersUseCase struct {
	repo out.OwnerRepository
}

func NewListOwnersUseCase(repo out.OwnerRepository) *ListOwnersUseCase {
	return &ListOwnersUseCase{repo: repo}
}

func (uc *ListOwnersUseCase) Execute(ctx context.Context) ([]owner.Owner, error) {
	owners, err := uc.repo.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	if owners == nil {
		owners = []owner.Owner{}
	}
	return owners, nil
}
