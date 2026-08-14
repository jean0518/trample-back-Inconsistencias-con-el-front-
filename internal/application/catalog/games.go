package catalog

import (
	"context"
	"trample-back/internal/domain/catalog"
	"trample-back/internal/ports/out"
)

type GamesUseCase struct {
	repo out.GameRepository
}

func NewGamesUseCase(repo out.GameRepository) *GamesUseCase {
	return &GamesUseCase{repo: repo}
}

func (uc *GamesUseCase) ListGames(ctx context.Context) ([]catalog.Game, error) {
	return uc.repo.ListGames(ctx)
}
