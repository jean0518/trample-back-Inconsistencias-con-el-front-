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
	// El handler resuelve el propietario (provisto o predeterminado); aquí solo
	// se garantiza que venga uno válido. La FK de inventory_listings.owner_id
	// respalda la integridad si el ID no existe.
	if input.OwnerID <= 0 {
		return listing.Listing{}, fmt.Errorf("owner_id es requerido")
	}
	rate, err := uc.trm.GetRate(ctx)
	if err != nil {
		return listing.Listing{}, fmt.Errorf("obtener TRM: %w", err)
	}
	input.PriceCOP = listing.StandardizedPriceCOP(input.PriceUSD, rate)
	return uc.repo.Create(ctx, input)
}
