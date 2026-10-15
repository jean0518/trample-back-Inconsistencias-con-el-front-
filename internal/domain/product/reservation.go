package product

import (
	"errors"
	"time"
)

// ReservationHoldMinutes es lo que dura la reserva de un producto en el carrito,
// igual que las cartas.
const ReservationHoldMinutes = 5

var (
	// ErrInvalidQuantity: la cantidad de una línea del carrito debe ser ≥ 1
	// (para quitarla está DELETE).
	ErrInvalidQuantity = errors.New("la cantidad debe ser al menos 1")
	// ErrCheckoutInProgress: la línea está en un pago abierto y no se puede
	// editar mientras tanto.
	ErrCheckoutInProgress = errors.New("la reserva tiene un pago en curso")
)

// Reservation es el stock de un producto apartado temporalmente para un
// cliente mientras lo tiene en el carrito. No descuenta el stock: el producto
// se consume solo al confirmarse la venta.
type Reservation struct {
	ID        int64
	UserID    string
	ProductID int64
	Title     string
	Quantity  int
	// MaxQuantity es el tope al que se puede llevar esta línea: el stock menos
	// lo que tienen apartado los demás (incluye lo que ya aparta esta reserva).
	MaxQuantity int
	PriceCOP    int64
	ImageURL    string
	ExpiresAt   time.Time
}

// ReserveInput es lo que se quiere apartar.
type ReserveInput struct {
	UserID    string
	ProductID int64
	Quantity  int
}
