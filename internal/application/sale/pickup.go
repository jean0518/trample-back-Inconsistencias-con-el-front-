package sale

import (
	"context"
	"log/slog"
	"time"

	"trample-back/internal/domain/sale"
	"trample-back/internal/ports/out"
)

// PickupOrdersUseCase gestiona los pedidos de recogida en tienda una vez
// apartados: registrar el pago en el local, cancelarlos y cerrar los que no se
// recogieron a tiempo.
type PickupOrdersUseCase struct {
	sales    out.SaleRepository
	notifier *PurchaseNotifier
	now      func() time.Time
}

func NewPickupOrdersUseCase(sales out.SaleRepository, notifier *PurchaseNotifier) *PickupOrdersUseCase {
	return &PickupOrdersUseCase{sales: sales, notifier: notifier, now: time.Now}
}

// MarkPaid registra que el cliente pagó y recogió el pedido en la tienda.
func (uc *PickupOrdersUseCase) MarkPaid(ctx context.Context, id int64) (sale.Sale, error) {
	s, err := uc.sales.MarkPickupPaid(ctx, id)
	if err != nil {
		return sale.Sale{}, err
	}
	// Es la primera y única vez que el pedido pasa a pagado: el repositorio no
	// deja repetir la transición, así que el correo no se duplica.
	s.JustCreated = true
	uc.notifier.OrderConfirmed(s)
	return s, nil
}

// Cancel cancela un pedido pendiente (decisión del staff) y devuelve su stock.
func (uc *PickupOrdersUseCase) Cancel(ctx context.Context, id int64) (sale.Sale, error) {
	s, err := uc.sales.CancelPickup(ctx, id)
	if err != nil {
		return sale.Sale{}, err
	}
	uc.notifier.PickupCancelled(s, false)
	return s, nil
}

// ExpireOverdue cancela los pedidos cuyo plazo de recogida venció, devuelve su
// stock y avisa a los clientes. Devuelve cuántos canceló; lo ejecuta un bucle
// periódico. Si alguno falla, los demás igual se procesan.
func (uc *PickupOrdersUseCase) ExpireOverdue(ctx context.Context) (int, error) {
	cancelled, err := uc.sales.ExpirePickups(ctx, uc.now())
	for _, s := range cancelled {
		slog.Info("pedido de recogida vencido y cancelado", slog.Int64("sale_id", s.ID))
		uc.notifier.PickupCancelled(s, true)
	}
	return len(cancelled), err
}
