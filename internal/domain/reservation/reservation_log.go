package reservation

import "time"

const (
	// LogReserved: el cliente agregó la carta al carrito (reserva activa).
	LogReserved = "reserved"
	// LogReturned: la reserva se devolvió al stock (la eliminó o expiró).
	LogReturned = "returned"
	// LogSold: el cliente confirmó el pedido y la carta se vendió.
	LogSold = "sold"
)

// ReservationLog es una entrada del historial de reservas del carrito.
// Registra cada cambio de estado de una reserva con los datos del cliente y
// el detalle de las cartas en el momento del evento.
type ReservationLog struct {
	ID            int64
	ReservationID int64
	UserID        int64
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
	CreatedAt     time.Time
}
