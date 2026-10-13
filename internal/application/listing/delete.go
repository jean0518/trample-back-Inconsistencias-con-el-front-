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

func (uc *DeleteListingUseCase) Execute(ctx context.Context, id int64, sellerID, role string, perms []string) error {
	// El admin y el staff con acceso al inventario general pueden borrar
	// listings de cualquier vendedor; el resto solo los propios.
	return uc.repo.Delete(ctx, id, sellerID, hasGlobalInventoryAccess(role, perms))
}
