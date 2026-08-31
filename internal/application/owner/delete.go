package owner

import (
	"context"

	"trample-back/internal/ports/out"
)

type DeleteOwnerUseCase struct {
	repo out.OwnerRepository
}

func NewDeleteOwnerUseCase(repo out.OwnerRepository) *DeleteOwnerUseCase {
	return &DeleteOwnerUseCase{repo: repo}
}

func (uc *DeleteOwnerUseCase) Execute(ctx context.Context, id int64) error {
	return uc.repo.Delete(ctx, id)
}
