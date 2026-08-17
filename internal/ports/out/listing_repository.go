package out

import (
	"context"
	"trample-back/internal/domain/listing"
)

type ListingRepository interface {
	Create(ctx context.Context, input listing.CreateInput) (listing.Listing, error)
	ListBySeller(ctx context.Context, sellerID int64, limit, offset int) ([]listing.Listing, error)
	Delete(ctx context.Context, id, sellerID int64) error
}
