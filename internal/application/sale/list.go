package sale

import (
	"context"

	"trample-back/internal/domain/sale"
	"trample-back/internal/ports/out"
)

// ListSalesUseCase devuelve el historial de ventas: las del usuario (MySales) o
// las de todos con los filtros del panel (Search).
type ListSalesUseCase struct {
	repo out.SaleRepository
}

func NewListSalesUseCase(repo out.SaleRepository) *ListSalesUseCase {
	return &ListSalesUseCase{repo: repo}
}

func (uc *ListSalesUseCase) MySales(ctx context.Context, userID string, limit, offset int) ([]sale.Sale, error) {
	items, err := uc.repo.ListByUser(ctx, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	return items, nil
}

// Límites del listado del panel.
const (
	defaultSearchLimit = 50
	maxSearchLimit     = 500
)

// validFilterValues son los valores admitidos de cada filtro enumerado.
var validFilterValues = map[string]map[string]bool{
	"status":          {sale.StatusPaid: true, sale.StatusPendingPickup: true, sale.StatusCancelled: true, sale.StatusCredit: true},
	"shipping_status": {sale.ShippingPending: true, sale.ShippingShipped: true, sale.ShippingDelivered: true},
	"fulfillment":     {sale.FulfillmentPickup: true, sale.FulfillmentShipping: true},
	"payment_method":  {sale.PaymentBold: true, sale.PaymentCash: true, sale.PaymentTransfer: true, sale.PaymentStore: true, sale.PaymentCredit: true},
}

// validateFilter rechaza valores desconocidos en vez de devolver un listado
// vacío que parecería "no hay pedidos".
func validateFilter(f sale.ListFilter) error {
	for name, value := range map[string]string{
		"status": f.Status, "shipping_status": f.ShippingStatus,
		"fulfillment": f.Fulfillment, "payment_method": f.PaymentMethod,
	} {
		if value != "" && !validFilterValues[name][value] {
			return sale.ErrInvalidInput
		}
	}
	return nil
}

// Search lista las ventas del panel con filtros y devuelve el total sin paginar.
func (uc *ListSalesUseCase) Search(ctx context.Context, f sale.ListFilter) ([]sale.Sale, int, error) {
	if err := validateFilter(f); err != nil {
		return nil, 0, err
	}
	if f.Limit <= 0 {
		f.Limit = defaultSearchLimit
	}
	f.Limit = min(f.Limit, maxSearchLimit)
	f.Offset = max(f.Offset, 0)
	return uc.repo.Search(ctx, f)
}

// Counts cuenta los pedidos por etapa con los mismos filtros del listado (el de
// estado se ignora: cada tarjeta es un estado).
func (uc *ListSalesUseCase) Counts(ctx context.Context, f sale.ListFilter) (sale.StageCounts, error) {
	f.Status = ""
	if err := validateFilter(f); err != nil {
		return sale.StageCounts{}, err
	}
	return uc.repo.CountByStage(ctx, f)
}
