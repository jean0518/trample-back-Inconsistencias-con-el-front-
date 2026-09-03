package listing

import (
	"context"
	"trample-back/internal/domain/auth"
	"trample-back/internal/domain/listing"
	"trample-back/internal/ports/out"
)

type ListListingsUseCase struct {
	repo out.ListingRepository
}

func NewListListingsUseCase(repo out.ListingRepository) *ListListingsUseCase {
	return &ListListingsUseCase{repo: repo}
}

// Execute devuelve el inventario. Los administradores ven el listing general de
// todos los vendedores; el resto de usuarios solo sus propios listings.
func (uc *ListListingsUseCase) Execute(ctx context.Context, role string, sellerID int64, limit, offset int) ([]listing.Listing, error) {
	if limit <= 0 {
		limit = 50
	}
	if role == auth.RoleAdmin {
		return uc.repo.ListAll(ctx, limit, offset)
	}
	return uc.repo.ListBySeller(ctx, sellerID, limit, offset)
}