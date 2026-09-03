package sale

import (
	"context"

	"trample-back/internal/domain/sale"
	"trample-back/internal/ports/out"
)

// ListSalesUseCase devuelve el historial de ventas. Con
// `all=true` lista todas (uso admin); en caso contrario solo las del usuario.
type ListSalesUseCase struct {
	repo out.SaleRepository
}

func NewListSalesUseCase(repo out.SaleRepository) *ListSalesUseCase {
	return &ListSalesUseCase{repo: repo}
}

func (uc *ListSalesUseCase) MySales(ctx context.Context, userID int64, limit, offset int) ([]sale.Sale, error) {
	items, err := uc.repo.ListByUser(ctx, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	return items, nil
}

func (uc *ListSalesUseCase) AllSales(ctx context.Context, limit, offset int) ([]sale.Sale, error) {
	items, err := uc.repo.ListAll(ctx, limit, offset)
	if err != nil {
		return nil, err
	}
	return items, nil
}
