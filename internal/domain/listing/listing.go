package listing

import (
	"errors"
	"time"
)

// ErrNotFound indica que el listing no existe o no pertenece al vendedor.
var ErrNotFound = errors.New("listing no encontrado")

type Listing struct {
	ID        int64
	SellerID  int64
	VariantID int64
	OwnerID   int64
	GameName  string
	// Datos de la carta para el inventario legible (JOIN con cards/expansions).
	CardID int64
	// ExternalID y CardNumber identifican la versión concreta de la carta
	// (dos cartas pueden compartir nombre pero diferir en número, p. ej.
	// "Ahri, Inquisitive" 119 vs 119a).
	ExternalID string
	CardNumber string
	CardName   string
	CardImage  string
	ExpansionName string
	VariantName   string
	OwnerName     string
	// SellerName es el nombre del usuario que creó/administra el listing
	// (JOIN con users por seller_id).
	SellerName string
	Quantity      int
	PriceUSD      float64
	PriceCOP      float64
	Status        string
	Language      string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type CreateInput struct {
	SellerID  int64
	VariantID int64
	OwnerID   int64
	Quantity  int
	PriceUSD  float64
	// PriceCOP se calcula con la TRM del día antes de persistir.
	PriceCOP float64
	Language string
}

// UpdateStockInput modifica la cantidad de un listing. La regla de estado es
// automática: quantity 0 ⇒ 'inactive'; volver a subir stock ⇒ 'active'.
// Los listings solo manejan los estados 'active' o 'inactive'.
type UpdateStockInput struct {
	ID       int64
	SellerID int64
	Quantity int
}
