package listing

import (
	"context"
	"trample-back/internal/domain/listing"
	"trample-back/internal/ports/out"
)

type CreateListingUseCase struct {
	repo out.ListingRepository
}

func NewCreateListingUseCase(repo out.ListingRepository) *CreateListingUseCase {
	return &CreateListingUseCase{repo: repo}
}

func (uc *CreateListingUseCase) Execute(ctx context.Context, input listing.CreateInput) (listing.Listing, error) {
	return uc.repo.Create(ctx, input)
}
