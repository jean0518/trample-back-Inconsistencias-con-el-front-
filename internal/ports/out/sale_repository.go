package out

import (
	"context"
	"time"

	"trample-back/internal/domain/payment"
	"trample-back/internal/domain/sale"
)

// SaleRepository persiste ventas locales y sus items, incluido el ciclo de
// vida de los pagos en línea con Bold.
//
// La regla que atraviesa toda la interfaz: una venta (fila en `sales`) solo
// existe cuando el pago entró. Mientras el cliente paga, lo que hay es un
// CheckoutIntent; ApprovePayment es el único camino que lo convierte en venta.
type SaleRepository interface {
	// Create persiste la venta y sus items en una transacción atómica,
	// descontando el inventario (ventas ya pagadas: efectivo o transferencia).
	Create(ctx context.Context, s sale.Sale) (sale.Sale, error)

	// CreateCheckout registra una intención de pago para la pasarela. NO crea
	// ninguna venta: solo guarda la referencia que Bold necesita junto al
	// snapshot del pedido y las reservas que lo retienen.
	CreateCheckout(ctx context.Context, c sale.CheckoutIntent) (sale.CheckoutIntent, error)
	// FindOpenCheckoutByUser devuelve el checkout abierto (aún sin pagar) del
	// usuario, para reutilizar su referencia si vuelve a pulsar "Pagar" en vez
	// de acumular checkouts huérfanos. Devuelve sale.ErrPaymentNotFound si no
	// tiene ninguno.
	FindOpenCheckoutByUser(ctx context.Context, userID string) (sale.CheckoutIntent, error)
	// FindCheckoutByReference devuelve la intención de pago de esa referencia.
	FindCheckoutByReference(ctx context.Context, reference string) (sale.CheckoutIntent, error)
	// ExtendCheckout corre la expiración de un checkout abierto. Va siempre
	// junto a la extensión de sus reservas: si solo se moviera el stock, el
	// barrido podría liberar las cartas mientras el cliente sigue pagando.
	ExtendCheckout(ctx context.Context, reference string, expiresAt time.Time) (sale.CheckoutIntent, error)

	// ApprovePayment convierte el checkout en una venta: inserta el pedido y
	// sus items, confirma sus reservas y consume el inventario, todo en una
	// sola transacción. Es idempotente: si el pago ya se había aplicado, no
	// cambia nada. La venta se crea marcada RequiresReview cuando el pago no
	// llegó en condiciones normales: el checkout ya no estaba abierto (expiró,
	// falló o se reemplazó), el stock ya se había liberado, o `paidCOP` (el monto
	// que la pasarela cobró, 0 si se desconoce) no coincide con el total del pedido.
	ApprovePayment(ctx context.Context, reference, paymentID string, paidCOP int64) (sale.Sale, error)
	// RejectPayment marca el checkout como fallido y libera sus reservas para
	// que el stock vuelva a estar disponible. Nunca crea una venta.
	RejectPayment(ctx context.Context, reference string) (sale.CheckoutIntent, error)
	// CloseOpenCheckouts cierra todos los checkouts abiertos del usuario SIN
	// liberar sus reservas: se llama cuando el carrito cambió y el checkout
	// anterior dejó de servir, y esas mismas reservas sostendrán el nuevo.
	// Devuelve cuántos cerró.
	CloseOpenCheckouts(ctx context.Context, userID string) (int64, error)
	// ExpireCheckouts cierra los checkouts que el cliente no pagó una vez
	// pasada la ventana de retención más `grace`, y libera sus reservas. El
	// margen deja terminar los pagos que ya estaban en curso al vencer (p. ej.
	// PSE). Devuelve cuántos cerró.
	ExpireCheckouts(ctx context.Context, grace time.Duration) ([]sale.CheckoutIntent, error)
	// MarkCheckoutPending registra que el pago quedó en proceso en la pasarela y
	// alarga la retención del checkout hasta `window` desde su creación (el
	// stock, `grace` más). Solo actúa la primera vez, con el checkout abierto y
	// dentro de la ventana; devuelve true en ese caso.
	MarkCheckoutPending(ctx context.Context, reference string, window, grace time.Duration) (sale.CheckoutIntent, bool, error)
	// ListUnsettledCheckouts devuelve las referencias de los pagos que todavía
	// pueden haberse cobrado sin que lo sepamos: abiertos ya vencidos o con el
	// pago en proceso, y cerrados por vencimiento o rechazo en las últimas horas. Es la entrada de la
	// reconciliación con la pasarela.
	ListUnsettledCheckouts(ctx context.Context, limit int) ([]string, error)

	// RecordPaymentEvent guarda una notificación de la pasarela. Devuelve true
	// si es la primera vez que se ve (idempotencia); false si ya existía.
	RecordPaymentEvent(ctx context.Context, evt payment.Event) (bool, error)
	// ForgetPaymentEvent borra el registro de una notificación cuyo procesamiento
	// falló, para que el reintento de la pasarela se trate como la primera vez y
	// no se descarte como duplicado.
	ForgetPaymentEvent(ctx context.Context, eventID string) error
	// FlagSaleForReview marca para revisión manual la venta de esa referencia de
	// pago (p. ej. una anulación de un pago ya aprobado). No hace nada si el pago
	// no llegó a ser venta.
	FlagSaleForReview(ctx context.Context, reference string) error

	// MarkPickupPaid registra el pago en tienda de un pedido pendiente de
	// recogida (pasa a 'paid'). Devuelve sale.ErrNotPendingPickup si ya se pagó o
	// canceló, y sale.ErrPaymentNotFound si no existe.
	MarkPickupPaid(ctx context.Context, id int64) (sale.Sale, error)
	// CancelPickup cancela un pedido pendiente de recogida y devuelve su stock al
	// inventario. Mismos errores que MarkPickupPaid.
	CancelPickup(ctx context.Context, id int64) (sale.Sale, error)
	// ExpirePickups cancela los pedidos pendientes cuyo plazo venció en `now` y
	// devuelve los que canceló. Cada uno se procesa por separado.
	ExpirePickups(ctx context.Context, now time.Time) ([]sale.Sale, error)
	// RequestPickupCancel registra que el cliente `userID` pidió cancelar su
	// pedido de recogida (el stock sigue apartado). Errores: ErrForbidden si el
	// pedido no es suyo, ErrNotPendingPickup y ErrCancelAlreadyRequested.
	RequestPickupCancel(ctx context.Context, id int64, userID string) (sale.Sale, error)
	// RejectPickupCancel descarta la solicitud de cancelación (ErrNoCancelRequest
	// si no había una). La aprobación es CancelPickup.
	RejectPickupCancel(ctx context.Context, id int64) (sale.Sale, error)
	// ClaimPickupReminders marca y devuelve los pedidos de recogida a los que
	// toca recordarles el plazo. Cada pedido se devuelve una sola vez.
	ClaimPickupReminders(ctx context.Context, now time.Time) ([]sale.Sale, error)

	// MarkShipped registra el despacho de un pedido pagado con envío
	// (ErrNotShippable si no aplica, p. ej. requiere revisión).
	MarkShipped(ctx context.Context, id int64, carrier, tracking string) (sale.Sale, error)
	// MarkDelivered registra la entrega de un pedido ya enviado.
	MarkDelivered(ctx context.Context, id int64) (sale.Sale, error)
	// ConfirmReview quita la marca de revisión (ErrNotInReview si no la tenía).
	ConfirmReview(ctx context.Context, id int64) (sale.Sale, error)

	// FindByID devuelve una venta con sus items. Se usa para leer el pedido que
	// dejó ApprovePayment, cuya referencia ya solo vive en el checkout.
	FindByID(ctx context.Context, id int64) (sale.Sale, error)
	ListByUser(ctx context.Context, userID string, limit, offset int) ([]sale.Sale, error)
	// Search lista las ventas con los filtros del panel y devuelve el total de
	// resultados sin paginar.
	Search(ctx context.Context, f sale.ListFilter) ([]sale.Sale, int, error)
	// CountByStage cuenta pedidos e importe por etapa (pendiente, por enviar,
	// enviado, cerrado, cancelado) con los filtros indicados.
	CountByStage(ctx context.Context, f sale.ListFilter) (sale.StageCounts, error)
	// Stats agrega las ventas en buckets desde `since` hasta ahora. period
	// admite "day" (agrupa por día) o "week" (agrupa por semana ISO).
	Stats(ctx context.Context, period string, since time.Time) ([]sale.StatBucket, error)
}
