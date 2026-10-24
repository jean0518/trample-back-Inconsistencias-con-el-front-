package sale

import (
	"context"
	"strings"

	"trample-back/internal/domain/sale"
	"trample-back/internal/ports/out"
)

// AdminOrdersUseCase corrige pedidos ya creados desde el panel: datos de
// entrega, líneas, precios y estado.
type AdminOrdersUseCase struct {
	sales    out.SaleRepository
	notifier *PurchaseNotifier
}

func NewAdminOrdersUseCase(sales out.SaleRepository, notifier *PurchaseNotifier) *AdminOrdersUseCase {
	return &AdminOrdersUseCase{sales: sales, notifier: notifier}
}

func (uc *AdminOrdersUseCase) Update(ctx context.Context, in sale.UpdateOrderInput) (sale.Sale, error) {
	if in.ID <= 0 {
		return sale.Sale{}, sale.ErrInvalidInput
	}
	in.Status = strings.TrimSpace(in.Status)
	switch in.Status {
	case "", sale.StatusPaid, sale.StatusPendingPickup, sale.StatusCancelled:
	case sale.StatusCredit:
		// El crédito se crea con su propio endpoint; no se activa editando.
		return sale.Sale{}, sale.ErrInvalidInput
	default:
		return sale.Sale{}, sale.ErrInvalidInput
	}
	if err := validateOrderItems(in.Items); err != nil {
		return sale.Sale{}, err
	}
	if err := validateOrderProducts(in.ProductItems); err != nil {
		return sale.Sale{}, err
	}

	previous, err := uc.sales.FindByID(ctx, in.ID)
	if err != nil {
		return sale.Sale{}, err
	}
	updated, err := uc.sales.UpdateOrder(ctx, in)
	if err != nil {
		return sale.Sale{}, err
	}
	if previous.Status == updated.Status {
		return updated, nil
	}
	switch updated.Status {
	case sale.StatusPaid:
		updated.JustCreated = true
		uc.notifier.OrderConfirmed(updated)
	case sale.StatusCancelled:
		reason := out.CancelByStore
		if !updated.CancelRequestedAt.IsZero() {
			reason = out.CancelRequested
		}
		uc.notifier.PickupCancelled(updated, reason)
	}
	return updated, nil
}

func validateOrderItems(items []sale.OrderItemInput) error {
	seen := make(map[int64]bool, len(items))
	for _, it := range items {
		if it.Quantity < 0 || it.PriceCOP < 0 || it.PriceUSD < 0 {
			return sale.ErrInvalidInput
		}
		if it.ID > 0 {
			if seen[it.ID] {
				return sale.ErrInvalidInput
			}
			seen[it.ID] = true
			continue
		}
		if it.ListingID == nil || it.Quantity <= 0 {
			return sale.ErrInvalidInput
		}
	}
	return nil
}

func validateOrderProducts(items []sale.OrderProductInput) error {
	seen := make(map[int64]bool, len(items))
	for _, it := range items {
		if it.Quantity < 0 || it.PriceCOP < 0 {
			return sale.ErrInvalidInput
		}
		if it.ID > 0 {
			if seen[it.ID] {
				return sale.ErrInvalidInput
			}
			seen[it.ID] = true
			continue
		}
		if it.ProductID == nil || it.Quantity <= 0 {
			return sale.ErrInvalidInput
		}
	}
	return nil
}
