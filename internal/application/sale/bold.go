package sale

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"trample-back/internal/domain/payment"
	"trample-back/internal/domain/sale"
	"trample-back/internal/ports/out"
)

// Tipos de evento que envía la pasarela.
const (
	EventSaleApproved = "SALE_APPROVED"
	EventSaleRejected = "SALE_REJECTED"
	EventVoidApproved = "VOID_APPROVED"
	EventVoidRejected = "VOID_REJECTED"
)

// CheckPaymentStatusUseCase valida el pago de un checkout consultando a la
// pasarela y, si el pago entró, creando el pedido. Es la vía principal en el
// ambiente de pruebas de Bold, donde los webhooks no se envían automáticamente,
// y el respaldo cuando el cliente vuelve del checkout y el webhook aún no llegó.
//
// Es idempotente: el usuario puede invocarlo tantas veces como quiera. Un pago
// aprobado crea el pedido la primera vez y las siguientes devuelven ese mismo
// pedido sin duplicarlo.
type CheckPaymentStatusUseCase struct {
	sales    out.SaleRepository
	gateway  out.PaymentGateway
	notifier *PurchaseNotifier
}

func NewCheckPaymentStatusUseCase(sales out.SaleRepository, gateway out.PaymentGateway) *CheckPaymentStatusUseCase {
	return &CheckPaymentStatusUseCase{sales: sales, gateway: gateway}
}

// WithNotifier activa el correo de confirmación al crearse el pedido.
func (uc *CheckPaymentStatusUseCase) WithNotifier(n *PurchaseNotifier) *CheckPaymentStatusUseCase {
	uc.notifier = n
	return uc
}

// Execute consulta el estado del pago de `reference` y deja todo coherente con
// el resultado: si el pago entró, el pedido ya existe y lo devuelve en
// `Status.Sale`; si el cliente sigue pagando, `Status.Sale` viene vacío porque
// todavía no hay ningún pedido.
func (uc *CheckPaymentStatusUseCase) Execute(ctx context.Context, userID, reference string, isAdmin bool) (sale.PaymentStatus, error) {
	if reference == "" {
		return sale.PaymentStatus{}, sale.ErrPaymentNotFound
	}

	checkout, err := uc.sales.FindCheckoutByReference(ctx, reference)
	if err != nil {
		return sale.PaymentStatus{}, err
	}
	// Solo el dueño del checkout (o un admin) puede consultarlo: la referencia
	// viaja en la URL de redirección y no debe filtrar estados de otros pedidos.
	if !isAdmin && checkout.UserID != userID {
		return sale.PaymentStatus{}, sale.ErrForbidden
	}

	status := sale.PaymentStatus{Reference: reference, Status: checkout.Status}

	// El pago ya entró y el pedido ya existe: no hay nada que consultar.
	if checkout.Status == sale.CheckoutPaid && checkout.SaleID != 0 {
		s, err := uc.sales.FindByID(ctx, checkout.SaleID)
		if err != nil {
			if errors.Is(err, sale.ErrPaymentNotFound) {
				return status, nil
			}
			return sale.PaymentStatus{}, err
		}
		status.Sale = &s
		return status, nil
	}

	// Un checkout vencido o rechazado también se consulta a la pasarela: si el
	// pago entró después de que liberáramos el stock, hay que crear el pedido
	// igual (marcado para revisión manual).
	tx, err := uc.gateway.GetStatus(ctx, reference)
	if err != nil {
		if errors.Is(err, payment.ErrNotConfigured) {
			return sale.PaymentStatus{}, err
		}
		// Si la pasarela no responde se devuelve el estado local: el pago puede
		// haberse aprobado igual y llegar por webhook o por la reconciliación.
		slog.Warn("consultar estado del pago en la pasarela",
			slog.String("reference", reference), slog.Any("error", err))
		return status, nil
	}

	switch tx.Status {
	case payment.StatusApproved:
		// El pedido se crea aunque el cliente cierre la pestaña a mitad de la
		// petición: cancelar el contexto revertiría la transacción con el dinero ya
		// cobrado.
		created, err := uc.sales.ApprovePayment(context.WithoutCancel(ctx), reference, tx.PaymentID, tx.AmountCOP)
		if err != nil {
			return sale.PaymentStatus{}, err
		}
		logIfNeedsReview(created)
		uc.notifier.OrderConfirmed(created)
		return sale.PaymentStatus{Reference: reference, Status: sale.CheckoutPaid, Sale: &created}, nil
	case payment.StatusRejected, payment.StatusFailed:
		// Un intento rechazado NO cierra el checkout: el cliente puede reintentar
		// con otra tarjeta en el mismo pago, y si se liberara el stock aquí, un pago
		// aprobado después llegaría sin cartas (y otro cliente pudo haberlas
		// comprado). El checkout y su stock vencen solos por tiempo. Se informa el
		// intento fallido aparte para que el frontend lo muestre.
		status.GatewayStatus = tx.Status
		return status, nil
	case payment.StatusVoided:
		// La transacción se anuló: ya no es un pago válido.
		updated, err := uc.sales.RejectPayment(ctx, reference)
		if err != nil {
			return sale.PaymentStatus{}, err
		}
		// Se avisa una sola vez: la reconciliación vuelve a consultar los
		// checkouts fallidos, y para entonces ya estaba en failed.
		if checkout.Status != sale.CheckoutFailed {
			uc.notifier.PaymentFailed(checkout.UserID, out.PaymentFailure{Reference: reference, TotalCOP: checkout.TotalCOP, Voided: true})
		}
		return sale.PaymentStatus{Reference: reference, Status: updated.Status, GatewayStatus: tx.Status}, nil
	case payment.StatusProcessing, payment.StatusPending:
		// El pago está en proceso (PSE, transferencia bancaria): el pedido se da
		// por recibido y se espera hasta PendingPaymentWindow. Si no se aprueba
		// en ese tiempo, ExpireCheckouts lo cancela y devuelve el stock.
		c, first, err := uc.sales.MarkCheckoutPending(ctx, reference, sale.PendingPaymentWindow, paymentGrace)
		if err != nil {
			slog.Warn("registrar pago en proceso", slog.String("reference", reference), slog.Any("error", err))
			return status, nil
		}
		if first {
			slog.Info("pago en proceso: pedido recibido", slog.String("reference", reference))
			uc.notifier.PaymentPending(c)
		}
		status.GatewayStatus = tx.Status
		return status, nil
	default:
		// not_found o desconocido: el cliente sigue en la pasarela. Se devuelve
		// el estado local sin cambios; el webhook o una consulta posterior lo
		// reconciliarán.
		return status, nil
	}
}

// ProcessPaymentEventUseCase aplica una notificación de la pasarela (webhook).
type ProcessPaymentEventUseCase struct {
	sales    out.SaleRepository
	gateway  out.PaymentGateway
	notifier *PurchaseNotifier
}

func NewProcessPaymentEventUseCase(sales out.SaleRepository, gateway out.PaymentGateway) *ProcessPaymentEventUseCase {
	return &ProcessPaymentEventUseCase{sales: sales, gateway: gateway}
}

// WithNotifier activa los correos de pago aprobado, rechazado y anulado.
func (uc *ProcessPaymentEventUseCase) WithNotifier(n *PurchaseNotifier) *ProcessPaymentEventUseCase {
	uc.notifier = n
	return uc
}

// Execute registra el evento y, solo si es la primera vez que llega, aplica el
// cambio de estado. Devuelve true si el evento se procesó.
//
// La idempotencia es obligatoria: la pasarela reintenta la misma notificación
// hasta 5 veces y cada intento debe ser inocuo. Pero también debe ser
// recuperable: si aplicar el cambio falla, el evento se olvida y se devuelve el
// error (el handler responde 500), de modo que el reintento de la pasarela sí se
// procese. Si el evento se quedara registrado, ese reintento se descartaría como
// duplicado y el cliente habría pagado sin pedido.
func (uc *ProcessPaymentEventUseCase) Execute(ctx context.Context, evt payment.Event) (bool, error) {
	first, err := uc.sales.RecordPaymentEvent(ctx, evt)
	if err != nil {
		return false, err
	}
	if !first {
		// Ya lo habíamos procesado: se responde 200 sin tocar nada.
		return false, nil
	}
	// Sin referencia no hay checkout local que actualizar (p. ej. pagos hechos
	// fuera de la tienda); el evento queda registrado y se responde 200.
	if evt.Reference == "" {
		return false, nil
	}

	if err := uc.apply(ctx, evt); err != nil {
		if ferr := uc.sales.ForgetPaymentEvent(context.WithoutCancel(ctx), evt.ID); ferr != nil {
			slog.Error("olvidar evento de pago fallido",
				slog.String("event_id", evt.ID), slog.Any("error", ferr))
		}
		return true, err
	}
	return true, nil
}

func (uc *ProcessPaymentEventUseCase) apply(ctx context.Context, evt payment.Event) error {
	switch evt.Type {
	case EventSaleApproved:
		// Nunca se confía solo en la notificación: se confirma con la pasarela. Así
		// un evento falsificado (en pruebas la firma usa una llave vacía y cualquiera
		// podría generarla) no crea un pedido, y el monto cobrado sale de la fuente
		// fiable.
		tx, err := uc.gateway.GetStatus(ctx, evt.Reference)
		if err != nil {
			return fmt.Errorf("confirmar pago de %s con la pasarela: %w", evt.Reference, err)
		}
		if tx.Status != payment.StatusApproved {
			return fmt.Errorf("pago de %s (estado %q): %w", evt.Reference, tx.Status, payment.ErrNotConfirmed)
		}
		paymentID := evt.PaymentID
		if paymentID == "" {
			paymentID = tx.PaymentID
		}
		paid := tx.AmountCOP
		if paid == 0 {
			paid = evt.AmountCOP
		}
		created, err := uc.sales.ApprovePayment(ctx, evt.Reference, paymentID, paid)
		if err != nil {
			return fmt.Errorf("aprobar pago de %s: %w", evt.Reference, err)
		}
		logIfNeedsReview(created)
		uc.notifier.OrderConfirmed(created)
	case EventSaleRejected:
		// Un intento rechazado no cierra el checkout: el cliente puede reintentar
		// con otra tarjeta y el stock sigue retenido hasta que venza. Liberarlo aquí
		// dejaría sin cartas a un pago que se aprueba después.
		slog.Info("intento de pago rechazado", slog.String("reference", evt.Reference))
		uc.notifyFailure(ctx, evt.Reference, false)
	case EventVoidApproved:
		// Una anulación aprobada deja de ser un pago válido. Si el checkout seguía
		// abierto se libera el stock; si el pedido ya existía, el reembolso es un
		// proceso manual y el pedido se marca para que no se despache por error.
		if _, err := uc.sales.RejectPayment(ctx, evt.Reference); err != nil {
			return fmt.Errorf("anular pago de %s: %w", evt.Reference, err)
		}
		if err := uc.sales.FlagSaleForReview(ctx, evt.Reference); err != nil {
			return err
		}
		slog.Warn("pago anulado en la pasarela: revisar el pedido", slog.String("reference", evt.Reference))
		uc.notifyFailure(ctx, evt.Reference, true)
		uc.notifyVoidedSale(ctx, evt.Reference)
	case EventVoidRejected:
		// La anulación falló: el pago sigue vigente. No hay nada que cambiar.
	}
	return nil
}

// notifyFailure avisa al cliente de un pago que salió mal. Si el checkout ya se
// pagó (un intento rechazado que llega después de uno aprobado) no se avisa: el
// cliente tiene su pedido y el correo solo lo confundiría. Un fallo al buscarlo
// no se propaga: el correo es accesorio y no debe hacer reintentar el webhook.
func (uc *ProcessPaymentEventUseCase) notifyFailure(ctx context.Context, reference string, voided bool) {
	if uc.notifier == nil {
		return
	}
	c, err := uc.sales.FindCheckoutByReference(ctx, reference)
	if err != nil {
		slog.Warn("correo de pago fallido: buscar checkout", slog.String("reference", reference), slog.Any("error", err))
		return
	}
	if c.Status == sale.CheckoutPaid && !voided {
		return
	}
	uc.notifier.PaymentFailed(c.UserID, out.PaymentFailure{Reference: reference, TotalCOP: c.TotalCOP, Voided: voided})
}

// notifyVoidedSale avisa a los admins si el pago anulado ya se había convertido
// en pedido: hay que frenar el despacho. Como notifyFailure, no propaga errores.
func (uc *ProcessPaymentEventUseCase) notifyVoidedSale(ctx context.Context, reference string) {
	if uc.notifier == nil {
		return
	}
	c, err := uc.sales.FindCheckoutByReference(ctx, reference)
	if err != nil || c.SaleID == 0 {
		return
	}
	s, err := uc.sales.FindByID(ctx, c.SaleID)
	if err != nil {
		slog.Warn("aviso de pago anulado: buscar pedido", slog.String("reference", reference), slog.Any("error", err))
		return
	}
	uc.notifier.PaymentVoided(s)
}

// logIfNeedsReview deja constancia de los pagos aprobados que requieren que una
// persona los revise (pago tardío, monto distinto, stock ya liberado).
func logIfNeedsReview(s sale.Sale) {
	if s.RequiresReview {
		slog.Warn("pago aprobado que requiere revisión manual",
			slog.Int64("sale_id", s.ID), slog.String("reference", s.PaymentReference))
	}
}

// ExpireCheckoutsUseCase cierra los checkouts que el cliente no llegó a pagar
// dentro de la ventana de retención, devolviendo el stock retenido. Lo ejecuta
// un bucle periódico desde el arranque de la API.
type ExpireCheckoutsUseCase struct {
	sales    out.SaleRepository
	notifier *PurchaseNotifier
}

func NewExpireCheckoutsUseCase(sales out.SaleRepository) *ExpireCheckoutsUseCase {
	return &ExpireCheckoutsUseCase{sales: sales}
}

// WithNotifier activa el correo de "pedido cancelado" a los clientes cuyo pago
// quedó en proceso y no se confirmó a tiempo.
func (uc *ExpireCheckoutsUseCase) WithNotifier(n *PurchaseNotifier) *ExpireCheckoutsUseCase {
	uc.notifier = n
	return uc
}

// Execute devuelve cuántos checkouts cerró. Espera `paymentGraceMinutes` más allá
// del vencimiento para no soltar el stock de un pago que aún se está resolviendo.
//
// Solo se avisa a quien ya había recibido "pedido recibido" (pago en proceso):
// un checkout que el cliente abrió y abandonó nunca fue un pedido para él.
func (uc *ExpireCheckoutsUseCase) Execute(ctx context.Context) (int64, error) {
	expired, err := uc.sales.ExpireCheckouts(ctx, paymentGrace)
	for _, c := range expired {
		if !c.PaymentPendingAt.IsZero() {
			uc.notifier.PaymentFailed(c.UserID, out.PaymentFailure{Reference: c.Reference, TotalCOP: c.TotalCOP, Expired: true})
		}
	}
	return int64(len(expired)), err
}

// ReconcileCheckoutsUseCase pregunta a la pasarela por los pagos que pueden
// haberse cobrado sin que el backend se enterara (webhook perdido, servidor
// reiniciado a mitad de proceso, pago pendiente que se resolvió tarde). Es la red
// de seguridad: sin ella, esos pagos se quedan sin pedido hasta que el cliente
// vuelva al carrito.
type ReconcileCheckoutsUseCase struct {
	sales  out.SaleRepository
	status *CheckPaymentStatusUseCase
}

func NewReconcileCheckoutsUseCase(sales out.SaleRepository, status *CheckPaymentStatusUseCase) *ReconcileCheckoutsUseCase {
	return &ReconcileCheckoutsUseCase{sales: sales, status: status}
}

// reconcileBatch acota las consultas a la pasarela por pasada.
const reconcileBatch = 100

// Execute devuelve cuántos pagos recuperó (cobrados en la pasarela y sin pedido).
// Reutiliza CheckPaymentStatusUseCase, que ya es idempotente.
func (uc *ReconcileCheckoutsUseCase) Execute(ctx context.Context) (int, error) {
	refs, err := uc.sales.ListUnsettledCheckouts(ctx, reconcileBatch)
	if err != nil {
		return 0, err
	}
	recovered := 0
	for _, ref := range refs {
		st, err := uc.status.Execute(ctx, "", ref, true)
		if err != nil {
			if errors.Is(err, payment.ErrNotConfigured) {
				return recovered, err
			}
			slog.Error("reconciliar pago", slog.String("reference", ref), slog.Any("error", err))
			continue
		}
		if st.Status == sale.CheckoutPaid {
			recovered++
			slog.Warn("pago recuperado por reconciliación", slog.String("reference", ref))
		}
	}
	return recovered, nil
}
