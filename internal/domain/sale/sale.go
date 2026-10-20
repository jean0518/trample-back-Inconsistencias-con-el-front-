package sale

import (
	"errors"
	"strconv"
	"time"
)

var ErrEmptyCart = errors.New("el carrito no tiene items para confirmar")
var ErrInvalidInput = errors.New("datos de la venta inválidos")
var ErrReservationExpired = errors.New("una o más reservas del carrito expiraron o ya no están activas")
var ErrPaymentNotFound = errors.New("no hay una venta con esa referencia de pago")
var ErrNotPendingPickup = errors.New("el pedido no está pendiente de recogida en tienda")
var ErrForbidden = errors.New("no tienes acceso a esa venta")

// ErrEmailNotVerified se devuelve cuando el cliente intenta comprar sin haber
// verificado su correo: sin un correo válido no le llegarían los avisos del
// pedido y la tienda no tendría cómo contactarlo.
var ErrEmailNotVerified = errors.New("verifica tu correo antes de comprar")

// ErrCancelAlreadyRequested se devuelve si el pedido ya tiene una solicitud de
// cancelación pendiente.
var ErrCancelAlreadyRequested = errors.New("ya solicitaste cancelar este pedido")

// ErrNoCancelRequest se devuelve al aprobar o rechazar una cancelación que el
// cliente no solicitó.
var ErrNoCancelRequest = errors.New("el pedido no tiene una solicitud de cancelación")

// ErrNotInReview se devuelve al confirmar un pedido que no estaba marcado para
// revisión.
var ErrNotInReview = errors.New("el pedido no está pendiente de revisión")

// ErrNotShippable se devuelve al marcar como enviado o entregado un pedido que
// no está en el estado correcto: no es de envío, no está pagado, ya se envió o
// entregó, o requiere revisión (p. ej. un pago anulado: no se despacha).
var ErrNotShippable = errors.New("el pedido no se puede despachar en su estado actual")

// ErrInsufficientStock se devuelve cuando, al registrar la venta, un listing ya no
// tiene las unidades que se iban a vender (alguien editó el inventario mientras
// la carta estaba reservada).
var ErrInsufficientStock = errors.New("uno o más artículos ya no tienen stock suficiente")

// ErrCheckoutConflict se devuelve cuando ya existe un pago abierto para el
// usuario y no corresponde a este carrito (dos "Pagar" simultáneos).
var ErrCheckoutConflict = errors.New("ya hay un pago en curso, inténtalo de nuevo en un momento")

const (
	FulfillmentPickup   = "pickup"
	FulfillmentShipping = "shipping"

	PaymentCash     = "efectivo"
	PaymentTransfer = "transferencia"
	PaymentBold     = "bold"
	// PaymentStore es el pedido de recogida que se paga en el local.
	PaymentStore = "tienda"

	// Estados de una venta. Una venta solo existe cuando el pago entró
	// (`paid`), así sea en efectivo, por transferencia o aprobado por la
	// pasarela. Un pago en línea que aún no se aprueba NO es una venta: es un
	// CheckoutIntent (ver abajo).
	StatusPaid = "paid"
	// StatusPendingPickup es un pedido de recogida en tienda aún sin pagar: el
	// stock está apartado y el cliente tiene hasta PickupDeadline para pagar y
	// recogerlo. Es la única excepción a "una venta solo existe si está pagada".
	StatusPendingPickup = "pending_pickup"
	// StatusCancelled es un pedido de recogida que no se recogió a tiempo (o que
	// el staff canceló); su stock ya volvió al inventario.
	StatusCancelled = "cancelled"

	// PickupBusinessDays son los días hábiles que el cliente tiene para pagar y
	// recoger un pedido en tienda.
	PickupBusinessDays = 2

	// PendingPaymentWindow es lo máximo que se espera un pago en proceso (PSE,
	// transferencia bancaria) desde que se creó el checkout. Si no se aprueba en
	// ese tiempo, el pedido se cancela y el stock vuelve al inventario.
	PendingPaymentWindow = 2 * time.Hour

	// ShippingCostCOP es el cargo fijo de envío a domicilio. Se suma al
	// total del pedido solo cuando fulfillment = shipping.
	ShippingCostCOP int64 = 25000
)

// Sale representa el registro local de una venta/pedido. Una venta solo existe
// cuando el pago entró, así que `status` siempre es StatusPaid; se conserva el
// campo para trazabilidad y por las ventas del flujo anterior.
type Sale struct {
	ID     int64
	UserID string
	// CustomerName y CustomerEmail son del comprador, leídos de users al
	// consultar (no se guardan en la venta). Vacíos en una venta recién creada.
	CustomerName  string
	CustomerEmail string
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
	// ProductItems son las líneas de productos propios de la tienda (módulo de
	// productos). Van aparte de Items porque no tienen listing ni carta.
	ProductItems []ProductItem

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
	// ProductReservationIDs son las reservas de productos que la sostuvieron.
	ProductReservationIDs []int64
	// PickupDeadline es el límite para recoger un pedido pendiente en tienda
	// (cero si no aplica).
	PickupDeadline time.Time
	// CancelRequestedAt es cuándo el cliente pidió cancelar un pedido de
	// recogida (cero si no lo pidió). El pedido sigue apartado hasta que el staff
	// apruebe o rechace la solicitud.
	CancelRequestedAt time.Time
	// Despacho de los pedidos con envío a domicilio (cero/vacío si no aplica).
	ShippedAt      time.Time
	Carrier        string
	TrackingNumber string
	// DeliveredAt es cuándo el pedido llegó al cliente: entregado por la
	// transportadora o recogido en la tienda.
	DeliveredAt time.Time
	// JustCreated es true solo en la llamada que creó el pedido (no se guarda):
	// permite avisar al cliente una sola vez aunque el pago llegue por varias vías.
	JustCreated bool
}

// Reference es como se identifica el pedido ante el cliente: "#<id>".
func (s Sale) Reference() string { return "#" + strconv.FormatInt(s.ID, 10) }

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
	// Snapshot de la variante (Normal, Foil…), el juego y la expansión al
	// vender: dicen qué copia despachar y permiten desglosar las ventas. El
	// juego y la expansión se resuelven al guardar la línea.
	VariantName   string `json:"variant_name,omitempty"`
	GameCode      string `json:"game_code,omitempty"`
	GameName      string `json:"game_name,omitempty"`
	ExpansionName string `json:"expansion_name,omitempty"`
}

// ProductItem es una línea de producto de la venta: snapshot del título y el
// precio al vender, para que el historial no cambie si el producto se edita.
// ProductID es nil si el producto se eliminó.
type ProductItem struct {
	ProductID *int64 `json:"product_id"`
	Title     string `json:"title"`
	Quantity  int    `json:"quantity"`
	PriceCOP  int64  `json:"price_cop"`
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
	// ProductItems y ProductReservationIDs son lo mismo para los productos.
	ProductItems          []ProductItem
	ProductReservationIDs []int64
	Status                string
	// SaleID es el pedido creado al aprobarse el pago; 0 mientras no exista.
	SaleID        int64
	BoldPaymentID string
	CreatedAt     string
	PaidAt        time.Time
	ExpiresAt     time.Time
	// PaymentPendingAt es cuándo se supo que el pago quedó en proceso en la
	// pasarela (cero si nunca lo estuvo). Se le avisó al cliente que el pedido se
	// recibió y se espera el pago hasta PendingPaymentWindow.
	PaymentPendingAt time.Time
}

// AsSale arma una vista del checkout con forma de pedido, para los correos que
// se envían antes de que exista uno (pago en proceso o vencido).
func (c CheckoutIntent) AsSale() Sale {
	return Sale{
		UserID: c.UserID, TotalCOP: c.TotalCOP, ShippingCOP: c.ShippingCOP,
		Fulfillment: c.Fulfillment, Address: c.Address, City: c.City, Phone: c.Phone,
		PaymentMethod: c.PaymentMethod, PaymentReference: c.Reference,
		Items: c.Items, ProductItems: c.ProductItems,
	}
}

// PaymentStatus es el resultado de preguntarle a la pasarela por un pago. Sale
// solo lleva el pedido cuando el pago ya entró: mientras el cliente está
// pagando todavía no existe ningún pedido.
type PaymentStatus struct {
	Reference string
	// Status es uno de los estados de CheckoutIntent.
	Status string
	// GatewayStatus es el último estado que informó la pasarela cuando no se
	// refleja en Status: un intento rechazado deja el checkout abierto (el cliente
	// puede reintentar con otra tarjeta), pero el frontend necesita saber que el
	// intento falló. Vacío si no aporta nada.
	GatewayStatus string
	Sale          *Sale
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
	// ProductReservationIDs son las reservas de productos del carrito que se
	// pagan junto con las cartas (el módulo de productos tiene carrito propio).
	ProductReservationIDs []int64
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

// Etapas del pedido en el panel de pedidos. Se derivan del estado de la venta y
// del despacho; no se guardan.
const (
	StagePending   = "pending"   // recogida apartada, sin pagar
	StageToShip    = "to_ship"   // pagado con envío, sin despachar
	StageShipped   = "shipped"   // despachado, sin entregar
	StageClosed    = "closed"    // recogida pagada, o envío entregado
	StageCancelled = "cancelled" // recogida cancelada o vencida
)

// Estados del despacho de un pedido pagado con envío (shipping_status).
const (
	ShippingPending   = "pending"
	ShippingShipped   = "shipped"
	ShippingDelivered = "delivered"
)

// ListFilter son los filtros del listado de ventas del panel. Los campos vacíos
// no filtran.
type ListFilter struct {
	// From y To acotan created_at: [From, To). Cero = sin límite.
	From, To       time.Time
	Status         string
	ShippingStatus string
	Fulfillment    string
	PaymentMethod  string
	// Query busca en el #id, el nombre o correo del cliente, el teléfono, la
	// ciudad y los nombres de cartas y productos.
	Query string
	// Ascending ordena del más antiguo al más nuevo (para despachar en orden).
	Ascending bool
	Limit     int
	Offset    int
}

// StageCount es cuántos pedidos hay en una etapa y cuánto suman.
type StageCount struct {
	Orders   int   `json:"orders"`
	TotalCOP int64 `json:"total_cop"`
}

// StageCounts son los conteos de las tarjetas del panel de pedidos.
type StageCounts struct {
	Pending   StageCount `json:"pending"`
	ToShip    StageCount `json:"to_ship"`
	Shipped   StageCount `json:"shipped"`
	Closed    StageCount `json:"closed"`
	Cancelled StageCount `json:"cancelled"`
}
