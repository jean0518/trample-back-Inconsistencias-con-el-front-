package sale

import (
	"errors"
	"time"
)

var ErrEmptyCart = errors.New("el carrito no tiene items para confirmar")
var ErrInvalidInput = errors.New("datos de la venta inválidos")
var ErrReservationExpired = errors.New("una o más reservas del carrito expiraron o ya no están activas")
var ErrPaymentNotFound = errors.New("no hay una venta con esa referencia de pago")
var ErrForbidden = errors.New("no tienes acceso a esa venta")

const (
	FulfillmentPickup   = "pickup"
	FulfillmentShipping = "shipping"

	PaymentCash     = "efectivo"
	PaymentTransfer = "transferencia"
	PaymentBold     = "bold"

	// Estados de una venta. Una venta solo existe cuando el pago entró
	// (`paid`), así sea en efectivo, por transferencia o aprobado por la
	// pasarela. Un pago en línea que aún no se aprueba NO es una venta: es un
	// CheckoutIntent (ver abajo).
	StatusPaid = "paid"

	// ShippingCostCOP es el cargo fijo de envío a domicilio. Se suma al
	// total del pedido solo cuando fulfillment = shipping.
	ShippingCostCOP int64 = 20000
)

// Sale representa el registro local de una venta/pedido. Una venta solo existe
// cuando el pago entró, así que `status` siempre es StatusPaid; se conserva el
// campo para trazabilidad y por las ventas del flujo anterior.
type Sale struct {
	ID            int64
	UserID        string
	TotalCOP      int64
	TotalUSD      float64
	ShippingCOP   int64
	ShippingUSD   float64
	Fulfillment   string
	Address       string
	City          string
	Phone         string
	PaymentMethod string
	Status        string
	CreatedAt     string
	Items         []SaleItem

	// Datos del pago en línea (Bold). Vacíos en ventas en efectivo o por
	// transferencia.
	PaymentReference string
	BoldPaymentID    string
	PaidAt           time.Time
	// RequiresReview marca los pagos que llegaron aprobados pero cuyo checkout
	// ya se había expirado y el stock se liberó (pago tardío): el dinero entró,
	// pero el inventario pudo asignarse a otro cliente y hay que revisarlo a
	// mano.
	RequiresReview bool
	// ReservationIDs son las reservas del carrito que sosteneron esta venta.
	ReservationIDs []int64
}

// SaleItem es una línea de la venta: un listing concreto que se vendió.
// ListingID puede ser nil si la carta (listing) fue eliminada del inventario;
// la venta se conserva gracias al snapshot de card_name/language/precios.
type SaleItem struct {
	ListingID *int64  `json:"listing_id"`
	CardID    int64   `json:"card_id"`
	CardName  string  `json:"card_name"`
	Language  string  `json:"language"`
	Quantity  int     `json:"quantity"`
	PriceCOP  int64   `json:"price_cop"`
	PriceUSD  float64 `json:"price_usd"`
}

// Estados de un CheckoutIntent. Recordan al estado de la venta para quien viene
// del flujo anterior, pero viven en su propia tabla.
const (
	// CheckoutOpen es un pago en curso: el cliente tiene el botón de la pasarela
	// abierto y el stock retenido por sus reservas del carrito.
	CheckoutOpen = "open"
	// CheckoutPaid significa que el pago entró y el pedido ya fue creado.
	CheckoutPaid = "paid"
	// CheckoutFailed es un pago rechazado o anulado por la pasarela.
	CheckoutFailed = "failed"
	// CheckoutExpired es un pago que el cliente nunca completó a tiempo.
	CheckoutExpired = "expired"
)

// CheckoutIntent es una intención de pago: lo mínimo que la pasarela necesita
// para cobrar (referencia, monto y datos del comprador) más el snapshot del
// pedido que se creará si el pago se aprueba.
//
// NO es un pedido. No aparece en el historial ni en las estadísticas, y solo
// puede pasar a ser uno (campo SaleID) cuando la pasarela confirma el pago.
type CheckoutIntent struct {
	Reference      string
	UserID         string
	TotalCOP       int64
	TotalUSD       float64
	ShippingCOP    int64
	ShippingUSD    float64
	Fulfillment    string
	Address        string
	City           string
	Phone          string
	PaymentMethod  string
	Items          []SaleItem
	ReservationIDs []int64
	Status         string
	// SaleID es el pedido creado al aprobarse el pago; 0 mientras no exista.
	SaleID        int64
	BoldPaymentID string
	CreatedAt     string
	PaidAt        time.Time
	ExpiresAt     time.Time
}

// PaymentStatus es el resultado de preguntarle a la pasarela por un pago. Sale
// solo lleva el pedido cuando el pago ya entró: mientras el cliente está
// pagando todavía no existe ningún pedido.
type PaymentStatus struct {
	Reference string
	// Status es uno de los estados de CheckoutIntent.
	Status string
	Sale   *Sale
}

// ConfirmInput describe los datos necesarios para registrar una venta local.
type ConfirmInput struct {
	UserID        string
	Fulfillment   string
	Address       string
	City          string
	Phone         string
	PaymentMethod string
	IsAdmin       bool
	// Datos del comprador tomados de la sesión (nunca del body), para
	// prellenar el formulario de la pasarela.
	PayerEmail string
	PayerName  string
}

// StatBucket agrega una ventana de tiempo (un día o una semana) dentro del
// dashboard de ventas.
type StatBucket struct {
	Label    string
	Start    string // fecha ISO del inicio del bucket (YYYY-MM-DD)
	Orders   int
	TotalCOP int64
	TotalUSD float64
}
