package listing

import (
	"context"
	"fmt"
	"trample-back/internal/domain/listing"
	"trample-back/internal/ports/out"
)

type UpdateStockUseCase struct {
	repo out.ListingRepository
}

func NewUpdateStockUseCase(repo out.ListingRepository) *UpdateStockUseCase {
	return &UpdateStockUseCase{repo: repo}
}

// Execute ajusta la cantidad de un listing del vendedor autenticado. La regla
// stock 0 ⇒ inactivo se aplica en el repositorio, atómicamente con el UPDATE.
func (uc *UpdateStockUseCase) Execute(ctx context.Context, input listing.UpdateStockInput) (listing.Listing, error) {
	if input.Quantity < 0 {
		return listing.Listing{}, fmt.Errorf("la cantidad no puede ser negativa")
	}
	return uc.repo.UpdateQuantity(ctx, input)
}
