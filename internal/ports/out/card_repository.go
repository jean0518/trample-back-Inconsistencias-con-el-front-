package out

import (
	"context"
	"time"
	"trample-back/internal/domain/catalog"
)

type ListCardsParams struct {
	GameCode    string
	ExpansionID int64
	Name        string
	Rarity      string
	OwnerID     int64
	Language    string
	MaxPrice    int64
	Sort        string
	CardID      int64
	Page        int
	PageSize    int
}

// StaleCardRef identifica una carta cuyo precio no se consulta en Scrydex
// hace más de un umbral dado (o nunca se consultó).
type StaleCardRef struct {
	ID         int64
	GameCode   string
	ExternalID string
}

type CardRepository interface {
	SyncCard(ctx context.Context, gameCode string, card catalog.Card) error
	// GetVariantID devuelve el ID de la variante persistida de una carta
	// (identificada por juego + ID externo). Se usa tras SyncCard para poder
	// crear listings apuntando a la variante correcta.
	GetVariantID(ctx context.Context, gameCode, externalID, variantName string) (int64, error)
	GetExternalID(ctx context.Context, id int64) (externalID string, err error)
	DeleteCard(ctx context.Context, id int64) error
	ListCards(ctx context.Context, p ListCardsParams) ([]catalog.CardSummary, int, error)
	// ListStaleCards devuelve cartas con inventario activo cuyo precio no se
	// actualiza hace más de olderThan (o nunca), las más viejas primero.
	ListStaleCards(ctx context.Context, olderThan time.Duration, limit int) ([]StaleCardRef, error)
}
