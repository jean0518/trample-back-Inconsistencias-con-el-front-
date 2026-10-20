package sale

import (
	"context"
	"log/slog"
	"strings"
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

// adminFinder devuelve los correos de los administradores, que reciben los
// avisos de pedidos. Se consulta en cada envío: un admin nuevo empieza a recibir
// los avisos sin reiniciar la API.
type adminFinder interface {
	ListAdminEmails(ctx context.Context) ([]string, error)
}

// PurchaseNotifier avisa por correo al cliente y a los administradores de lo que
// pasa con cada compra. Envía en segundo plano y solo registra los fallos: que
// un correo no salga nunca debe romper el checkout ni hacer que la pasarela
// reintente el webhook. Todos sus métodos son seguros con un notificador nil
// (no hace nada).
type PurchaseNotifier struct {
	users  userFinder
	mailer out.OrderMailer
	admins adminFinder
}

func NewPurchaseNotifier(users userFinder, mailer out.OrderMailer) *PurchaseNotifier {
	return &PurchaseNotifier{users: users, mailer: mailer}
}

// WithAdmins activa los avisos a los administradores.
func (n *PurchaseNotifier) WithAdmins(admins adminFinder) *PurchaseNotifier {
	n.admins = admins
	return n
}

// OrderConfirmed avisa del pedido recién creado. Ignora los pedidos que ya
// existían: ApprovePayment es idempotente y devuelve el mismo pedido al webhook
// repetido o a la reconciliación, y el cliente no debe recibir el correo dos veces.
//
// Un pedido que requiere revisión (el pago llegó tarde o con otro monto) no se
// confirma todavía: el cliente recibe "pedido recibido" y la confirmación llega
// cuando el staff lo revisa (ReviewConfirmed).
//
// Los admins reciben su aviso salvo en la recogida pagada en el local: ese
// pedido ya se les avisó al apartarse y quien lo marca como pagado es el staff.
func (n *PurchaseNotifier) OrderConfirmed(s sale.Sale) {
	if n == nil || !s.JustCreated {
		return
	}
	if s.RequiresReview {
		n.deliver(s.UserID, "pedido recibido en revisión", func(ctx context.Context, to, name string) error {
			return n.mailer.SendOrderReceived(ctx, to, name, s, out.ReceivedInReview)
		})
	} else {
		n.deliver(s.UserID, "confirmación de pedido", func(ctx context.Context, to, name string) error {
			return n.mailer.SendOrderConfirmed(ctx, to, name, s)
		})
	}
	if s.PaymentMethod != sale.PaymentStore {
		n.AdminNewOrder(s)
	}
}

// ReviewConfirmed envía la confirmación de un pedido que el staff ya revisó.
func (n *PurchaseNotifier) ReviewConfirmed(s sale.Sale) {
	if n == nil {
		return
	}
	n.deliver(s.UserID, "confirmación de pedido revisado", func(ctx context.Context, to, name string) error {
		return n.mailer.SendOrderConfirmed(ctx, to, name, s)
	})
}

// PaymentPending avisa que el pedido se recibió y el pago está en proceso.
func (n *PurchaseNotifier) PaymentPending(c sale.CheckoutIntent) {
	if n == nil {
		return
	}
	n.deliver(c.UserID, "pedido recibido con pago en proceso", func(ctx context.Context, to, name string) error {
		return n.mailer.SendOrderReceived(ctx, to, name, c.AsSale(), out.ReceivedPaymentPending)
	})
}

// PickupReserved avisa al cliente y a los admins de que el pedido de recogida
// quedó apartado.
func (n *PurchaseNotifier) PickupReserved(s sale.Sale) {
	if n == nil {
		return
	}
	n.deliver(s.UserID, "pedido apartado en tienda", func(ctx context.Context, to, name string) error {
		return n.mailer.SendPickupReserved(ctx, to, name, s)
	})
	n.AdminNewOrder(s)
}

// PickupReminder recuerda al cliente que el plazo de recogida vence pronto.
func (n *PurchaseNotifier) PickupReminder(s sale.Sale) {
	if n == nil {
		return
	}
	n.deliver(s.UserID, "recordatorio de recogida", func(ctx context.Context, to, name string) error {
		return n.mailer.SendPickupReminder(ctx, to, name, s)
	})
}

// PickupCancelled avisa al cliente de que el pedido de recogida se canceló. Si
// venció, también se avisa a los admins (las demás cancelaciones las hace el
// propio staff).
func (n *PurchaseNotifier) PickupCancelled(s sale.Sale, reason out.CancelReason) {
	if n == nil {
		return
	}
	n.deliver(s.UserID, "pedido de recogida cancelado", func(ctx context.Context, to, name string) error {
		return n.mailer.SendPickupCancelled(ctx, to, name, s, reason)
	})
	if reason == out.CancelExpired {
		n.adminAlert(s, out.AlertPickupExpired)
	}
}

// CancelRequested confirma al cliente su solicitud y pide a los admins que la
// aprueben o rechacen.
func (n *PurchaseNotifier) CancelRequested(s sale.Sale) {
	if n == nil {
		return
	}
	n.deliver(s.UserID, "solicitud de cancelación", func(ctx context.Context, to, name string) error {
		return n.mailer.SendCancelRequested(ctx, to, name, s)
	})
	n.adminAlert(s, out.AlertCancelRequested)
}

// CancelRejected avisa al cliente de que la cancelación no se aprobó.
func (n *PurchaseNotifier) CancelRejected(s sale.Sale) {
	if n == nil {
		return
	}
	n.deliver(s.UserID, "cancelación rechazada", func(ctx context.Context, to, name string) error {
		return n.mailer.SendCancelRejected(ctx, to, name, s)
	})
}

// OrderShipped avisa al cliente de que su pedido salió.
func (n *PurchaseNotifier) OrderShipped(s sale.Sale) {
	if n == nil {
		return
	}
	n.deliver(s.UserID, "pedido enviado", func(ctx context.Context, to, name string) error {
		return n.mailer.SendOrderShipped(ctx, to, name, s)
	})
}

// OrderDelivered avisa al cliente de que su pedido fue entregado.
func (n *PurchaseNotifier) OrderDelivered(s sale.Sale) {
	if n == nil {
		return
	}
	n.deliver(s.UserID, "pedido entregado", func(ctx context.Context, to, name string) error {
		return n.mailer.SendOrderDelivered(ctx, to, name, s)
	})
}

// PaymentFailed avisa de un pago rechazado, anulado o que no se confirmó a tiempo.
func (n *PurchaseNotifier) PaymentFailed(userID string, f out.PaymentFailure) {
	if n == nil {
		return
	}
	n.deliver(userID, "pago fallido", func(ctx context.Context, to, name string) error {
		return n.mailer.SendPaymentFailed(ctx, to, name, f)
	})
}

// PaymentVoided avisa a los admins de que se anuló el pago de un pedido ya
// creado: no debe despacharse.
func (n *PurchaseNotifier) PaymentVoided(s sale.Sale) {
	if n == nil {
		return
	}
	n.adminAlert(s, out.AlertPaymentVoided)
}

// AdminNewOrder avisa a los admins de un pedido nuevo. Se usa sola en las ventas
// que registra el staff (efectivo o transferencia): ahí el "comprador" es la
// cuenta del staff y no hay cliente al que escribirle.
func (n *PurchaseNotifier) AdminNewOrder(s sale.Sale) {
	if n == nil {
		return
	}
	n.toAdmins(s, "aviso de pedido nuevo", func(ctx context.Context, to string, buyer out.OrderBuyer) error {
		return n.mailer.SendAdminNewOrder(ctx, to, buyer, s)
	})
}

func (n *PurchaseNotifier) adminAlert(s sale.Sale, alert out.AdminAlert) {
	n.toAdmins(s, "alerta de pedido", func(ctx context.Context, to string, buyer out.OrderBuyer) error {
		return n.mailer.SendAdminAlert(ctx, to, buyer, s, alert)
	})
}

// toAdmins envía un correo a cada administrador con los datos del comprador.
func (n *PurchaseNotifier) toAdmins(s sale.Sale, kind string, send func(ctx context.Context, to string, buyer out.OrderBuyer) error) {
	if n.admins == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), mailTimeout)
		defer cancel()
		admins, err := n.admins.ListAdminEmails(ctx)
		if err != nil {
			slog.Error("correo: buscar administradores", slog.String("kind", kind), slog.Int64("sale_id", s.ID), slog.Any("error", err))
			return
		}
		if len(admins) == 0 {
			slog.Warn("correo: no hay administradores a quienes avisar", slog.String("kind", kind), slog.Int64("sale_id", s.ID))
			return
		}
		var buyer out.OrderBuyer
		if u, err := n.users.FindByID(ctx, s.UserID); err != nil {
			// El aviso sale igual: a los admins les sirve más un pedido sin
			// contacto que no enterarse.
			slog.Warn("correo: buscar comprador", slog.String("kind", kind), slog.String("user_id", s.UserID), slog.Any("error", err))
		} else {
			buyer = out.OrderBuyer{Name: strings.TrimSpace(u.FirstName + " " + u.LastName), Email: u.Email}
		}
		for _, to := range admins {
			if err := send(ctx, to, buyer); err != nil {
				slog.Error("correo: envío fallido", slog.String("kind", kind), slog.String("to", to), slog.Int64("sale_id", s.ID), slog.Any("error", err))
			}
		}
	}()
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
