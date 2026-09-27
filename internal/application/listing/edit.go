package listing

import (
	"context"
	"fmt"

	"trample-back/internal/domain/listing"
	"trample-back/internal/ports/out"
)

type EditListingUseCase struct {
	repo out.ListingRepository
	trm  out.TRMClient
}

func NewEditListingUseCase(repo out.ListingRepository, trm out.TRMClient) *EditListingUseCase {
	return &EditListingUseCase{repo: repo, trm: trm}
}

func (uc *EditListingUseCase) Execute(ctx context.Context, input listing.EditInput) (listing.Listing, error) {
	if input.Quantity < 0 {
		return listing.Listing{}, fmt.Errorf("la cantidad no puede ser negativa")
	}
	if input.PriceUSD <= 0 {
		return listing.Listing{}, fmt.Errorf("price_usd debe ser mayor a 0")
	}
	// La lista de idiomas aceptados vive acá y no solo en el front a
	// propósito: si únicamente el formulario la tuviera, un cliente podría
	// saltársela y metería cualquier texto en listings.language.
	switch input.Language {
	case "Inglés", "Español", "Japonés", "Chino":
	default:
		return listing.Listing{}, fmt.Errorf("language inválido: debe ser Inglés, Español, Japonés o Chino")
	}
	if input.OwnerID <= 0 {
		return listing.Listing{}, fmt.Errorf("owner_id es requerido")
	}
	rate, err := uc.trm.GetRate(ctx)
	if err != nil {
		return listing.Listing{}, fmt.Errorf("obtener TRM: %w", err)
	}
	input.PriceCOP = listing.StandardizedPriceCOP(input.PriceUSD, rate)
	return uc.repo.Edit(ctx, input)
}
