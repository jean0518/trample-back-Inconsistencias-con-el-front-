package listing

import (
	"context"
	"trample-back/internal/domain/listing"
	"trample-back/internal/ports/out"
)

type ListListingsUseCase struct {
	repo out.ListingRepository
}

func NewListListingsUseCase(repo out.ListingRepository) *ListListingsUseCase {
	return &ListListingsUseCase{repo: repo}
}

func (uc *ListListingsUseCase) Execute(ctx context.Context, sellerID int64, limit, offset int) ([]listing.Listing, error) {
	if limit <= 0 {
		limit = 50
	}
	return uc.repo.ListBySeller(ctx, sellerID, limit, offset)
}
