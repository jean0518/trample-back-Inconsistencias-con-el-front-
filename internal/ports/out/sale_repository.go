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
	// cambia nada. Si el checkout ya había expirado, la venta se crea igual pero
	// marcada RequiresReview (pago tardío: el stock ya se liberó).
	ApprovePayment(ctx context.Context, reference, paymentID string) (sale.Sale, error)
	// RejectPayment marca el checkout como fallido y libera sus reservas para
	// que el stock vuelva a estar disponible. Nunca crea una venta.
	RejectPayment(ctx context.Context, reference string) (sale.CheckoutIntent, error)
	// CloseOpenCheckouts cierra todos los checkouts abiertos del usuario y
	// libera el stock que retinían. Se llama cuando el carrito cambió y el
	// checkout anterior dejó de servir. Devuelve cuántos cerró.
	CloseOpenCheckouts(ctx context.Context, userID string) (int64, error)
	// ExpireCheckouts cierra los checkouts que el cliente no pagó dentro de la
	// ventana de retención y libera sus reservas. Devuelve cuántos cerró.
	ExpireCheckouts(ctx context.Context) (int64, error)

	// RecordPaymentEvent guarda una notificación de la pasarela. Devuelve true
	// si es la primera vez que se ve (idempotencia); false si ya existía.
	RecordPaymentEvent(ctx context.Context, evt payment.Event) (bool, error)

	// FindByID devuelve una venta con sus items. Se usa para leer el pedido que
	// dejó ApprovePayment, cuya referencia ya solo vive en el checkout.
	FindByID(ctx context.Context, id int64) (sale.Sale, error)
	ListByUser(ctx context.Context, userID string, limit, offset int) ([]sale.Sale, error)
	ListAll(ctx context.Context, limit, offset int) ([]sale.Sale, error)
	// Stats agrega las ventas en buckets desde `since` hasta ahora. period
	// admite "day" (agrupa por día) o "week" (agrupa por semana ISO).
	Stats(ctx context.Context, period string, since time.Time) ([]sale.StatBucket, error)
}
