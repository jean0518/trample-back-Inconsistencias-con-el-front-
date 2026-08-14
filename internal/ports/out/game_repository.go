package out

import (
	"context"
	"trample-back/internal/domain/catalog"
)

type GameRepository interface {
	ListGames(ctx context.Context) ([]catalog.Game, error)
}
