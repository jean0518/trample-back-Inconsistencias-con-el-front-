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

// canSeeAllListings indica si el usuario ve el inventario general de todos los
// vendedores: el admin siempre, y cualquier staff con el panel de inventario.
func canSeeAllListings(role string, perms []string) bool {
	if role == auth.RoleAdmin {
		return true
	}
	for _, p := range perms {
		if p == auth.PermInventario {
			return true
		}
	}
	return false
}

// Execute devuelve el inventario. El admin y el staff con el panel de
// inventario ven el listing general de todos los vendedores; el resto de
// usuarios solo sus propios listings.
func (uc *ListListingsUseCase) Execute(ctx context.Context, role string, perms []string, sellerID int64, limit, offset int) ([]listing.Listing, error) {
	if limit <= 0 {
		limit = 50
	}
	if canSeeAllListings(role, perms) {
		return uc.repo.ListAll(ctx, limit, offset)
	}
	return uc.repo.ListBySeller(ctx, sellerID, limit, offset)
}