package reservation

import (
	"errors"
	"time"
)

var ErrNotFound = errors.New("reserva no encontrada")
var ErrInsufficientStock = errors.New("stock insuficiente para reservar")
var ErrInvalidListing = errors.New("no hay listing activo disponible para esa carta e idioma")

const (
	StatusActive    = "active"
	StatusConfirmed = "confirmed"
	StatusReleased  = "released"
)

// Reservation representa el stock temporal de 5 minutos reservado para un
// cliente cuando agrega una carta a su carrito.
type Reservation struct {
	ID          int64
	UserID      int64
	ListingID   int64
	CardID      int64
	CardName    string
	VariantName string
	Language    string
	Quantity    int
	PriceUSD    float64
	PriceCOP    float64
	CardImage   string
	ExpiresAt   time.Time
	Status      string
	CreatedAt   time.Time
}

// ReserveInput describe lo que se quiere reservar: N unidades de una carta
// en un idioma concreto.
type ReserveInput struct {
	UserID   int64
	CardID   int64
	Language string
	Quantity int
}

// ListingInfo describe el listing activo elegido para reservar una carta en
// un idioma, con los datos de la carta para mostrarla en el carrito.
type ListingInfo struct {
	ListingID  int64
	CardID     int64
	CardName   string
	Language   string
	Quantity   int
	PriceUSD   float64
	PriceCOP   float64
	Stock      int
	CardImage  string
	VariantName string
	Status     string
}
