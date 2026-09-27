package listing

import (
	"context"
	"testing"

	"trample-back/internal/domain/listing"
	"trample-back/internal/ports/out"
)

type fakeListingRepo struct {
	out.ListingRepository
	updated *listing.UpdateStockInput
}

// sellerUUID representa el id de users.id, que es un UUID y por eso viaja como
// string en todo el dominio.
const sellerUUID = "3f1b7c2e-5a49-4d8b-9c6f-1e2a3b4c5d6e"

func (f *fakeListingRepo) UpdateQuantity(_ context.Context, input listing.UpdateStockInput) (listing.Listing, error) {
	f.updated = &input
	return listing.Listing{ID: input.ID, Quantity: input.Quantity, Status: "active"}, nil
}

func TestUpdateStockRechazaCantidadNegativa(t *testing.T) {
	repo := &fakeListingRepo{}
	uc := NewUpdateStockUseCase(repo)

	if _, err := uc.Execute(context.Background(), listing.UpdateStockInput{ID: 1, SellerID: sellerUUID, Quantity: -1}); err == nil {
		t.Fatal("esperaba error con cantidad negativa")
	}
	if repo.updated != nil {
		t.Fatal("no debe llamar al repo con cantidad negativa")
	}
}

func TestUpdateStockPermiteCero(t *testing.T) {
	repo := &fakeListingRepo{}
	uc := NewUpdateStockUseCase(repo)

	l, err := uc.Execute(context.Background(), listing.UpdateStockInput{ID: 1, SellerID: sellerUUID, Quantity: 0})
	if err != nil {
		t.Fatalf("stock 0 debe permitirse: %v", err)
	}
	if l.Quantity != 0 || repo.updated.Quantity != 0 {
		t.Fatal("la cantidad 0 no llegó al repo")
	}
}
