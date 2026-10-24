package out

import (
	"context"
	"trample-back/internal/domain/catalog"
)

type GameRepository interface {
	ListGames(ctx context.Context) ([]catalog.Game, error)
	ListGamesAdmin(ctx context.Context) ([]catalog.Game, error)
	GetByID(ctx context.Context, id int64) (catalog.Game, error)
	GetByCode(ctx context.Context, code string) (catalog.Game, error)
	Create(ctx context.Context, in catalog.GameInput) (catalog.Game, error)
	Update(ctx context.Context, id int64, in catalog.GameInput) (catalog.Game, error)
	DeleteWithProducts(ctx context.Context, id int64) (int64, error)
	SetActive(ctx context.Context, id int64, active bool) (catalog.Game, error)
	AttachIconImage(ctx context.Context, id int64, url string, imageID *int64) error
	AttachLogoImage(ctx context.Context, id int64, url string, imageID *int64) error
}
