package out

import (
	"context"
	"trample-back/internal/domain/listing"
)

type ListingRepository interface {
	Create(ctx context.Context, input listing.CreateInput) (listing.Listing, error)
	ListBySeller(ctx context.Context, sellerID int64, limit, offset int) ([]listing.Listing, error)
	UpdateQuantity(ctx context.Context, input listing.UpdateStockInput) (listing.Listing, error)
	// FindBySellerAndVariantLanguageOwner devuelve el listing vigente (active o
	// inactive) del vendedor para una variante con el mismo idioma y propietario,
	// o listing.ErrNotFound si no existe.
	FindBySellerAndVariantLanguageOwner(ctx context.Context, sellerID, variantID int64, language string, ownerID int64) (listing.Listing, error)
	// AddQuantity suma cantidad al listing indicado y lo reactiva si estaba
	// 'inactive'.
	AddQuantity(ctx context.Context, input listing.UpdateStockInput) (listing.Listing, error)
	Delete(ctx context.Context, id, sellerID int64) error
}
