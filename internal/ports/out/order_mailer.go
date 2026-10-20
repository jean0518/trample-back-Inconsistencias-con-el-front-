package out

import (
	"context"

	"trample-back/internal/domain/sale"
)

// OrderMailer envía los correos del ciclo de compra: al cliente y al staff.
type OrderMailer interface {
	// SendOrderConfirmed avisa que el pago entró y el pedido quedó confirmado
	// (pago en línea, o recogida pagada en la tienda).
	SendOrderConfirmed(ctx context.Context, to, name string, s sale.Sale) error
	// SendOrderReceived avisa que el pedido se recibió pero todavía no está
	// confirmado: el pago sigue en proceso o hay que confirmar la disponibilidad.
	// La confirmación llega después con SendOrderConfirmed.
	SendOrderReceived(ctx context.Context, to, name string, s sale.Sale, reason ReceivedReason) error
	// SendPickupReserved avisa que el pedido de recogida quedó apartado y que se
	// paga en la tienda antes de s.PickupDeadline.
	SendPickupReserved(ctx context.Context, to, name string, s sale.Sale) error
	// SendPickupReminder recuerda que el plazo para recoger vence pronto.
	SendPickupReminder(ctx context.Context, to, name string, s sale.Sale) error
	// SendPickupCancelled avisa que el pedido de recogida se canceló y por qué.
	SendPickupCancelled(ctx context.Context, to, name string, s sale.Sale, reason CancelReason) error
	// SendCancelRequested confirma que se recibió la solicitud de cancelación.
	SendCancelRequested(ctx context.Context, to, name string, s sale.Sale) error
	// SendCancelRejected avisa que la tienda no aprobó la cancelación.
	SendCancelRejected(ctx context.Context, to, name string, s sale.Sale) error
	// SendOrderShipped avisa que el pedido salió (transportadora y guía).
	SendOrderShipped(ctx context.Context, to, name string, s sale.Sale) error
	// SendOrderDelivered avisa que el pedido fue entregado.
	SendOrderDelivered(ctx context.Context, to, name string, s sale.Sale) error
	// SendPaymentFailed avisa que el pago no se completó (rechazado, anulado o
	// sin confirmar a tiempo).
	SendPaymentFailed(ctx context.Context, to, name string, f PaymentFailure) error

	// SendAdminNewOrder avisa al staff de un pedido nuevo (pagado o apartado
	// para recoger) con los datos del comprador para prepararlo.
	SendAdminNewOrder(ctx context.Context, to string, buyer OrderBuyer, s sale.Sale) error
	// SendAdminAlert avisa al staff de algo que debe atender en un pedido.
	SendAdminAlert(ctx context.Context, to string, buyer OrderBuyer, s sale.Sale, alert AdminAlert) error
}

// OrderBuyer son los datos de contacto del comprador que ve el staff.
type OrderBuyer struct {
	Name  string
	Email string
}

// ReceivedReason explica por qué un pedido recibido aún no está confirmado.
type ReceivedReason int

const (
	// ReceivedPaymentPending: la pasarela tiene el pago en proceso (PSE,
	// transferencia). Se espera hasta sale.PendingPaymentWindow.
	ReceivedPaymentPending ReceivedReason = iota
	// ReceivedInReview: el pago entró, pero tarde o con algo inesperado; el staff
	// confirma la disponibilidad antes de confirmar el pedido.
	ReceivedInReview
)

// CancelReason es el motivo de la cancelación de un pedido de recogida.
type CancelReason int

const (
	// CancelExpired: no se recogió dentro del plazo.
	CancelExpired CancelReason = iota
	// CancelByStore: la tienda lo canceló por su cuenta.
	CancelByStore
	// CancelRequested: la tienda aprobó la cancelación que pidió el cliente.
	CancelRequested
)

// AdminAlert es el tipo de aviso al staff sobre un pedido existente.
type AdminAlert int

const (
	// AlertCancelRequested: el cliente pidió cancelar; hay que aprobar o rechazar.
	AlertCancelRequested AdminAlert = iota
	// AlertPickupExpired: el pedido de recogida venció y su stock se liberó.
	AlertPickupExpired
	// AlertPaymentVoided: el pago de un pedido ya creado se anuló. No despachar.
	AlertPaymentVoided
)

// PaymentFailure es lo que el cliente necesita saber de un pago que salió mal.
type PaymentFailure struct {
	Reference string
	TotalCOP  int64
	// Voided es true cuando el pago se anuló después de aprobarse.
	Voided bool
	// Expired es true cuando el pago quedó en proceso y no se confirmó dentro
	// de sale.PendingPaymentWindow: el pedido se canceló.
	Expired bool
}
