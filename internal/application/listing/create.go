package listing

import (
	"context"
	"fmt"

	"trample-back/internal/domain/listing"
	"trample-back/internal/ports/out"
)

type CreateListingUseCase struct {
	repo out.ListingRepository
	trm  out.TRMClient
}

func NewCreateListingUseCase(repo out.ListingRepository, trm out.TRMClient) *CreateListingUseCase {
	return &CreateListingUseCase{repo: repo, trm: trm}
}

func (uc *CreateListingUseCase) Execute(ctx context.Context, input listing.CreateInput) (listing.Listing, error) {
	rate, err := uc.trm.GetRate(ctx)
	if err != nil {
		return listing.Listing{}, fmt.Errorf("obtener TRM: %w", err)
	}
	input.PriceCOP = listing.StandardizedPriceCOP(input.PriceUSD, rate)
	return uc.repo.Create(ctx, input)
}
