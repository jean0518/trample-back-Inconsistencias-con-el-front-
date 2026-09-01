package sale

import "errors"

var ErrNotFound = errors.New("venta no encontrada")
var ErrEmptyCart = errors.New("el carrito no tiene items para confirmar")
var ErrInvalidInput = errors.New("datos de la venta inválidos")
var ErrReservationExpired = errors.New("una o más reservas del carrito expiraron o ya no están activas")

const (
	FulfillmentPickup   = "pickup"
	FulfillmentShipping = "shipping"

	PaymentCash     = "efectivo"
	PaymentTransfer = "transferencia"

	StatusPaid = "paid"

	// ShippingCostCOP es el cargo fijo de envío a domicilio. Se suma al
	// total del pedido solo cuando fulfillment = shipping.
	ShippingCostCOP int64 = 20000
)

// Sale representa el registro local de una venta/pedido. No se conecta con
// ninguna pasarela de pagos externa (p.ej. Bold) todavía.
type Sale struct {
	ID            int64
	UserID        int64
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
}

// SaleItem es una línea de la venta: un listing concreto que se vendió.
// ListingID puede ser nil si la carta (listing) fue eliminada del inventario;
// la venta se conserva gracias al snapshot de card_name/language/precios.
type SaleItem struct {
	ID        int64
	SaleID    int64
	ListingID *int64
	CardID    int64
	CardName  string
	Language  string
	Quantity  int
	PriceCOP  int64
	PriceUSD  float64
}

// ConfirmInput describe los datos necesarios para registrar una venta local.
type ConfirmInput struct {
	UserID        int64
	Fulfillment   string
	Address       string
	City          string
	Phone         string
	PaymentMethod string
	IsAdmin       bool
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
