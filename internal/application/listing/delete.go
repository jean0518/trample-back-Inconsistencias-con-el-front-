package listing

import (
	"context"
	"trample-back/internal/ports/out"
)

type DeleteListingUseCase struct {
	repo out.ListingRepository
}

func NewDeleteListingUseCase(repo out.ListingRepository) *DeleteListingUseCase {
	return &DeleteListingUseCase{repo: repo}
}

func (uc *DeleteListingUseCase) Execute(ctx context.Context, id, sellerID int64) error {
	return uc.repo.Delete(ctx, id, sellerID)
}
