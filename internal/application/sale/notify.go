package sale

import (
	"context"
	"log/slog"
	"time"

	"trample-back/internal/domain/auth"
	"trample-back/internal/domain/sale"
	"trample-back/internal/ports/out"
)

// mailTimeout acota cada envío: un proveedor lento no debe acumular goroutines.
const mailTimeout = 20 * time.Second

type userFinder interface {
	FindByID(ctx context.Context, id string) (auth.User, error)
}

// PurchaseNotifier avisa por correo al cliente del resultado de su pago. Envía
// en segundo plano y solo registra los fallos: que un correo no salga nunca debe
// romper el checkout ni hacer que la pasarela reintente el webhook. Todos sus
// métodos son seguros con un notificador nil (no hace nada).
type PurchaseNotifier struct {
	users  userFinder
	mailer out.OrderMailer
}

func NewPurchaseNotifier(users userFinder, mailer out.OrderMailer) *PurchaseNotifier {
	return &PurchaseNotifier{users: users, mailer: mailer}
}

// OrderConfirmed avisa del pedido recién creado. Ignora los pedidos que ya
// existían: ApprovePayment es idempotente y devuelve el mismo pedido al webhook
// repetido o a la reconciliación, y el cliente no debe recibir el correo dos veces.
func (n *PurchaseNotifier) OrderConfirmed(s sale.Sale) {
	if n == nil || !s.JustCreated {
		return
	}
	n.deliver(s.UserID, "confirmación de pedido", func(ctx context.Context, to, name string) error {
		return n.mailer.SendOrderConfirmed(ctx, to, name, s)
	})
}

// PickupReserved avisa de que el pedido de recogida quedó apartado.
func (n *PurchaseNotifier) PickupReserved(s sale.Sale) {
	if n == nil {
		return
	}
	n.deliver(s.UserID, "pedido apartado en tienda", func(ctx context.Context, to, name string) error {
		return n.mailer.SendPickupReserved(ctx, to, name, s)
	})
}

// PickupCancelled avisa de que el pedido de recogida se canceló.
func (n *PurchaseNotifier) PickupCancelled(s sale.Sale, expired bool) {
	if n == nil {
		return
	}
	n.deliver(s.UserID, "pedido de recogida cancelado", func(ctx context.Context, to, name string) error {
		return n.mailer.SendPickupCancelled(ctx, to, name, s, expired)
	})
}

// PaymentFailed avisa de un pago rechazado o anulado.
func (n *PurchaseNotifier) PaymentFailed(userID string, f out.PaymentFailure) {
	if n == nil {
		return
	}
	n.deliver(userID, "pago fallido", func(ctx context.Context, to, name string) error {
		return n.mailer.SendPaymentFailed(ctx, to, name, f)
	})
}

func (n *PurchaseNotifier) deliver(userID, kind string, send func(ctx context.Context, to, name string) error) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), mailTimeout)
		defer cancel()
		u, err := n.users.FindByID(ctx, userID)
		if err != nil {
			slog.Error("correo: buscar destinatario", slog.String("kind", kind), slog.String("user_id", userID), slog.Any("error", err))
			return
		}
		if err := send(ctx, u.Email, u.FirstName); err != nil {
			slog.Error("correo: envío fallido", slog.String("kind", kind), slog.String("user_id", userID), slog.Any("error", err))
		}
	}()
}
