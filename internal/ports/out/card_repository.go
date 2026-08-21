package out

import (
	"context"
	"trample-back/internal/domain/catalog"
)

type ListCardsParams struct {
	GameCode    string
	ExpansionID int64
	Name        string
	Rarity      string
	Type        string
	Page        int
	PageSize    int
}

type CardRepository interface {
	SyncCard(ctx context.Context, gameCode string, card catalog.Card) error
	GetExternalID(ctx context.Context, id int64) (externalID string, err error)
	DeleteCard(ctx context.Context, id int64) error
	ListCards(ctx context.Context, p ListCardsParams) ([]catalog.CardSummary, int, error)
}
