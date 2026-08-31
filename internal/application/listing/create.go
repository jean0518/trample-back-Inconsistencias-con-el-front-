package listing

import (
	"context"
	"fmt"

	"trample-back/internal/domain/listing"
	"trample-back/internal/ports/out"
)

type CreateListingUseCase struct {
	repo   out.ListingRepository
	owners out.OwnerRepository
	trm    out.TRMClient
}

func NewCreateListingUseCase(repo out.ListingRepository, owners out.OwnerRepository, trm out.TRMClient) *CreateListingUseCase {
	return &CreateListingUseCase{repo: repo, owners: owners, trm: trm}
}

// DefaultOwnerID devuelve el ID del propietario marcado como predeterminado.
func (uc *CreateListingUseCase) DefaultOwnerID(ctx context.Context) (int64, error) {
	owners, err := uc.owners.ListAll(ctx)
	if err != nil {
		return 0, err
	}
	for _, o := range owners {
		if o.IsDefault {
			return o.ID, nil
		}
	}
	return 0, fmt.Errorf("no hay propietario por defecto configurado")
}

func (uc *CreateListingUseCase) Execute(ctx context.Context, input listing.CreateInput) (listing.Listing, error) {
	if input.OwnerID <= 0 {
		ownerID, err := uc.DefaultOwnerID(ctx)
		if err != nil {
			return listing.Listing{}, err
		}
		input.OwnerID = ownerID
	} else {
		// Validar que el propietario exista.
		if _, err := uc.owners.FindByID(ctx, input.OwnerID); err != nil {
			return listing.Listing{}, err
		}
	}
	rate, err := uc.trm.GetRate(ctx)
	if err != nil {
		return listing.Listing{}, fmt.Errorf("obtener TRM: %w", err)
	}
	input.PriceCOP = listing.StandardizedPriceCOP(input.PriceUSD, rate)
	return uc.repo.Create(ctx, input)
}
