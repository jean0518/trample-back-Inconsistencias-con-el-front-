package out

import (
	"context"
	"trample-back/internal/domain/listing"
)

type ListingRepository interface {
	Create(ctx context.Context, input listing.CreateInput) (listing.Listing, error)
	ListBySeller(ctx context.Context, sellerID int64, limit, offset int) ([]listing.Listing, error)
	UpdateQuantity(ctx context.Context, input listing.UpdateStockInput) (listing.Listing, error)
	// FindBySellerAndVariant devuelve el listing vigente (no 'sold') del
	// vendedor para una variante, o listing.ErrNotFound si no existe.
	FindBySellerAndVariant(ctx context.Context, sellerID, variantID int64) (listing.Listing, error)
	// AddQuantity suma cantidad al listing indicado y lo reactiva si estaba
	// 'inactive'. Nunca modifica un listing 'sold'.
	AddQuantity(ctx context.Context, input listing.UpdateStockInput) (listing.Listing, error)
	Delete(ctx context.Context, id, sellerID int64) error
}
