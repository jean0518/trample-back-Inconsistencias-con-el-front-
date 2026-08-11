package out

import (
	"context"
	"trample-back/internal/domain/catalog"
)

type CardRepository interface {
	SyncCard(ctx context.Context, gameCode string, card catalog.Card) error
}
