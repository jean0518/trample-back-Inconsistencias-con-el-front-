package out

import (
	"context"

	"trample-back/internal/domain/sale"
)

// OrderMailer envía al cliente los correos del ciclo de compra.
type OrderMailer interface {
	// SendOrderConfirmed avisa que el pago entró y el pedido quedó registrado.
	SendOrderConfirmed(ctx context.Context, to, name string, s sale.Sale) error
	// SendPickupReserved avisa que el pedido de recogida quedó apartado y que se
	// paga en la tienda antes de s.PickupDeadline.
	SendPickupReserved(ctx context.Context, to, name string, s sale.Sale) error
	// SendPickupCancelled avisa que el pedido de recogida se canceló. `expired`
	// distingue el vencimiento del plazo de una cancelación hecha por la tienda.
	SendPickupCancelled(ctx context.Context, to, name string, s sale.Sale, expired bool) error
	// SendPaymentFailed avisa que el pago no se completó (rechazado o anulado).
	SendPaymentFailed(ctx context.Context, to, name string, f PaymentFailure) error
}

// PaymentFailure es lo que el cliente necesita saber de un pago que salió mal.
type PaymentFailure struct {
	Reference string
	TotalCOP  int64
	// Voided es true cuando el pago se anuló después de aprobarse.
	Voided bool
}
