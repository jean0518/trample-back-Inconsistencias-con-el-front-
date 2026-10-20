package reservation

import (
	"errors"
	"time"
)

// ErrInvalidLogFilter se devuelve con un estado o tipo desconocido en los
// filtros del historial.
var ErrInvalidLogFilter = errors.New("filtro del historial de reservas inválido")

const (
	// LogReserved: el cliente agregó la carta al carrito (reserva activa).
	LogReserved = "reserved"
	// LogReturned: la reserva se devolvió al stock (la eliminó o expiró).
	LogReturned = "returned"
	// LogSold: el cliente confirmó el pedido y la carta se vendió.
	LogSold = "sold"
)

// Motivos de una devolución (LogReturned).
const (
	// ReturnRemoved: el cliente quitó la línea o bajó la cantidad.
	ReturnRemoved = "removed"
	// ReturnExpired: se acabó el tiempo del carrito o del pago en línea.
	ReturnExpired = "expired"
	// ReturnPaymentFailed: la pasarela rechazó o anuló el pago.
	ReturnPaymentFailed = "payment_failed"
)

// Tipos de reserva en el historial.
const (
	KindCard    = "card"
	KindProduct = "product"
)

// ReservationLog es una entrada del historial de reservas del carrito.
//
// Hay UNA FILA POR EVENTO, no una por reserva: una misma reserva genera
// `reserved` al agregarse, otra `reserved` por cada unidad que se suma, un
// `returned` por cada unidad que se quita, y termina con `sold` o `returned`.
// Quantity es la cantidad DE ESE EVENTO (en `sold` y en el `returned` final, lo
// que la reserva tenía en ese momento).
type ReservationLog struct {
	ID int64
	// Kind es card o product; las reservas de cartas y de productos tienen ids
	// independientes, así que una reserva se identifica por (Kind, ReservationID).
	Kind          string
	ReservationID int64
	UserID        string
	CustomerName  string
	CustomerEmail string
	CardID        int64
	CardName      string
	VariantName   string
	Language      string
	Quantity      int
	PriceUSD      float64
	PriceCOP      float64
	Status        string // reserved | returned | sold
	// ReturnReason explica un `returned` (removed | expired | payment_failed).
	// Vacío en los demás eventos y en los registros anteriores a este campo.
	ReturnReason string
	CreatedAt    time.Time

	// ProductID y Title identifican el producto en las reservas de productos.
	ProductID int64
	Title     string

	// Estado actual de la reserva (no del evento): cuándo vence si sigue
	// activa (cero si ya no) y si la retiene un pago en línea abierto.
	ExpiresAt  time.Time
	InCheckout bool
}

// LogFilter son los filtros del historial de reservas. Los vacíos no filtran.
type LogFilter struct {
	From, To time.Time // created_at en [From, To)
	Status   string
	Kind     string
	// Query busca en el cliente (nombre o correo) y en la carta o el producto.
	Query  string
	Limit  int
	Offset int
}
