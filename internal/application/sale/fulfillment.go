package sale

import (
	"context"
	"strings"

	"trample-back/internal/domain/sale"
	"trample-back/internal/ports/out"
)

const (
	maxCarrierLen  = 80
	maxTrackingLen = 80
)

// FulfillmentUseCase gestiona lo que pasa con un pedido pagado después de la
// compra: revisarlo si el pago llegó con algo raro, despacharlo y entregarlo.
// Cada paso le avisa al cliente.
type FulfillmentUseCase struct {
	sales    out.SaleRepository
	notifier *PurchaseNotifier
}

func NewFulfillmentUseCase(sales out.SaleRepository, notifier *PurchaseNotifier) *FulfillmentUseCase {
	return &FulfillmentUseCase{sales: sales, notifier: notifier}
}

// ConfirmReview marca como revisado un pedido que requería revisión (pago tardío
// o con otro monto) y le envía al cliente la confirmación que quedó pendiente.
func (uc *FulfillmentUseCase) ConfirmReview(ctx context.Context, id int64) (sale.Sale, error) {
	s, err := uc.sales.ConfirmReview(ctx, id)
	if err != nil {
		return sale.Sale{}, err
	}
	uc.notifier.ReviewConfirmed(s)
	return s, nil
}

// MarkShipped registra el despacho de un pedido con envío a domicilio. La
// transportadora es obligatoria; la guía es opcional (algunos envíos locales no
// tienen), pero se recomienda.
func (uc *FulfillmentUseCase) MarkShipped(ctx context.Context, id int64, carrier, tracking string) (sale.Sale, error) {
	carrier, tracking = strings.TrimSpace(carrier), strings.TrimSpace(tracking)
	if carrier == "" || len(carrier) > maxCarrierLen || len(tracking) > maxTrackingLen {
		return sale.Sale{}, sale.ErrInvalidInput
	}
	s, err := uc.sales.MarkShipped(ctx, id, carrier, tracking)
	if err != nil {
		return sale.Sale{}, err
	}
	uc.notifier.OrderShipped(s)
	return s, nil
}

// MarkDelivered registra la entrega de un pedido enviado.
func (uc *FulfillmentUseCase) MarkDelivered(ctx context.Context, id int64) (sale.Sale, error) {
	s, err := uc.sales.MarkDelivered(ctx, id)
	if err != nil {
		return sale.Sale{}, err
	}
	uc.notifier.OrderDelivered(s)
	return s, nil
}
