package sale

import (
	"context"
	"errors"
	"fmt"

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
	sales   out.SaleRepository
	gateway out.PaymentGateway
}

func NewCheckPaymentStatusUseCase(sales out.SaleRepository, gateway out.PaymentGateway) *CheckPaymentStatusUseCase {
	return &CheckPaymentStatusUseCase{sales: sales, gateway: gateway}
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
		// haberse aprobado igual y llegar por webhook.
		return status, nil
	}

	switch tx.Status {
	case payment.StatusApproved:
		created, err := uc.sales.ApprovePayment(ctx, reference, tx.PaymentID)
		if err != nil {
			return sale.PaymentStatus{}, err
		}
		return sale.PaymentStatus{Reference: reference, Status: sale.CheckoutPaid, Sale: &created}, nil
	case payment.StatusRejected, payment.StatusFailed, payment.StatusVoided:
		updated, err := uc.sales.RejectPayment(ctx, reference)
		if err != nil {
			return sale.PaymentStatus{}, err
		}
		return sale.PaymentStatus{Reference: reference, Status: updated.Status}, nil
	default:
		// processing, pending, not_found o desconocido: el pago sigue en
		// curso. Se devuelve el estado local sin cambios; el webhook o una
		// consulta posterior la reconciliarán.
		return status, nil
	}
}

// ProcessPaymentEventUseCase aplica una notificación de la pasarela (webhook).
type ProcessPaymentEventUseCase struct {
	sales out.SaleRepository
}

func NewProcessPaymentEventUseCase(sales out.SaleRepository) *ProcessPaymentEventUseCase {
	return &ProcessPaymentEventUseCase{sales: sales}
}

// Execute registra el evento y, solo si es la primera vez que llega, aplica el
// cambio de estado. Devuelve true si el evento se procesó.
//
// La idempotencia es obligatoria: la pasarela reintenta la misma notificación
// hasta 5 veces y cada intento debe ser inocuo. Además, aunque el evento se
// repita, ApprovePayment es idempotente por su cuenta.
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

	switch evt.Type {
	case EventSaleApproved:
		if _, err := uc.sales.ApprovePayment(ctx, evt.Reference, evt.PaymentID); err != nil {
			return true, fmt.Errorf("aprobar pago de %s: %w", evt.Reference, err)
		}
	case EventSaleRejected, EventVoidApproved, EventVoidRejected:
		// Un pago rechazado libera el stock de inmediato. Una anulación
		// (void) solo se aplica si el checkout seguía abierto: si el pago ya
		// se había aprobado y el pedido creado, el reembolso es un proceso
		// manual y no se toca aquí.
		if _, err := uc.sales.RejectPayment(ctx, evt.Reference); err != nil {
			return true, fmt.Errorf("rechazar pago de %s: %w", evt.Reference, err)
		}
	}
	return true, nil
}

// ExpireCheckoutsUseCase cierra los checkouts que el cliente no llegó a pagar
// dentro de la ventana de retención, devolviendo el stock retenido. Lo ejecuta
// un bucle periódico desde el arranque de la API.
type ExpireCheckoutsUseCase struct {
	sales out.SaleRepository
}

func NewExpireCheckoutsUseCase(sales out.SaleRepository) *ExpireCheckoutsUseCase {
	return &ExpireCheckoutsUseCase{sales: sales}
}

// Execute devuelve cuántos checkouts cerró.
func (uc *ExpireCheckoutsUseCase) Execute(ctx context.Context) (int64, error) {
	return uc.sales.ExpireCheckouts(ctx)
}
