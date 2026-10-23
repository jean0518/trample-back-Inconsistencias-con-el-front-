package reservation

import (
	"errors"
	"time"
)

var ErrNotFound = errors.New("reserva no encontrada")
var ErrInsufficientStock = errors.New("stock insuficiente para reservar")
var ErrInvalidListing = errors.New("no hay listing activo disponible para esa carta e idioma")

// ErrInvalidKind: el tipo de reserva no es card ni product.
var ErrInvalidKind = errors.New("tipo de reserva inválido")

// ErrInvalidQuantity: la cantidad de una línea del carrito debe ser ≥ 1 (para
// quitarla está Remove).
var ErrInvalidQuantity = errors.New("la cantidad debe ser al menos 1")

// ErrCheckoutInProgress: la reserva está en un pago abierto de Bold y no se
// puede editar mientras tanto.
var ErrCheckoutInProgress = errors.New("la reserva tiene un pago en curso")

// Reservation representa el stock temporal de 5 minutos reservado para un
// cliente cuando agrega una carta a su carrito.
type Reservation struct {
	ID          int64
	UserID      string
	ListingID   int64
	CardID      int64
	CardName    string
	VariantName string
	Language    string
	Quantity    int
	// MaxQuantity es el tope al que se puede llevar esta línea: el stock del
	// listing menos lo que tienen apartado los demás (incluye lo que ya aparta
	// esta reserva).
	MaxQuantity int
	PriceUSD    float64
	PriceCOP    float64
	CardImage   string
	ExpiresAt   time.Time
}

// ReserveInput describe lo que se quiere reservar: N unidades de una carta en
// un idioma concreto y, opcionalmente, el acabado (Normal, Foil, etc.). Si
// VariantName viene vacío, se reserva el listing activo con más stock.
type ReserveInput struct {
	UserID      string
	CardID      int64
	Language    string
	VariantName string
	Quantity    int
}

// ListingInfo describe el listing activo elegido para reservar una carta en
// un idioma, con los datos de la carta para mostrarla en el carrito.
type ListingInfo struct {
	ListingID   int64
	CardID      int64
	CardName    string
	Language    string
	Quantity    int
	PriceUSD    float64
	PriceCOP    float64
	CardImage   string
	VariantName string
}
