package out

import (
	"context"
	"trample-back/internal/domain/catalog"
)

type ExpansionRepository interface {
	SyncExpansions(ctx context.Context, gameCode string, expansions []catalog.Expansion) error
	ListExpansions(ctx context.Context, gameCode string) ([]catalog.Expansion, error)
	FindByName(ctx context.Context, gameCode, name string) (*catalog.Expansion, error)
}
