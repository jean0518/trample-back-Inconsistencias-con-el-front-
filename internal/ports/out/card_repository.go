package out

import (
	"context"
	"trample-back/internal/domain/catalog"
)

type CardRepository interface {
	SyncCard(ctx context.Context, gameCode string, card catalog.Card) error
	GetExternalID(ctx context.Context, id int64) (externalID string, err error)
	DeleteCard(ctx context.Context, id int64) error
}
