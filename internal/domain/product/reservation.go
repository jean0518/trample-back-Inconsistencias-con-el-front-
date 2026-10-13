package product

import "time"

// ReservationHoldMinutes es lo que dura la reserva de un producto en el carrito,
// igual que las cartas.
const ReservationHoldMinutes = 5

// Reservation es el stock de un producto apartado temporalmente para un
// cliente mientras lo tiene en el carrito. No descuenta el stock: el producto
// se consume solo al confirmarse la venta.
type Reservation struct {
	ID        int64
	UserID    string
	ProductID int64
	Title     string
	Quantity  int
	PriceCOP  int64
	ImageURL  string
	ExpiresAt time.Time
}

// ReserveInput es lo que se quiere apartar.
type ReserveInput struct {
	UserID    string
	ProductID int64
	Quantity  int
}
