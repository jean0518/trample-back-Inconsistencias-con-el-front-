package out

import (
	"context"
	"trample-back/internal/domain/catalog"
)

type ExpansionRepository interface {
	SyncExpansions(ctx context.Context, gameCode string, expansions []catalog.Expansion) error
	ListExpansions(ctx context.Context, gameCode string) ([]catalog.Expansion, error)
	ListByGameID(ctx context.Context, gameID int64) ([]catalog.Expansion, error)
	ListAll(ctx context.Context) ([]catalog.Expansion, error)
}
