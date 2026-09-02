package catalog

import (
	"context"
	"trample-back/internal/domain/catalog"
	"trample-back/internal/ports/out"
)

type ImportCardUseCase struct {
	search *SearchScrydex
	repo   out.CardRepository
}

// ImportGroup es un lote de cartas elegidas dentro de una búsqueda previa.
// La selección temporal del front puede acumular grupos de búsquedas distintas
// (otros juegos u otras expansiones) antes de importar todo junto.
func NewImportCardUseCase(search *SearchScrydex, repo out.CardRepository) *ImportCardUseCase {
	return &ImportCardUseCase{search: search, repo: repo}
}

func (uc *ImportCardUseCase) DeletePokemon(ctx context.Context, id int64) error {
	return uc.repo.DeleteCard(ctx, id)
}

func (uc *ImportCardUseCase) RefreshPokemon(ctx context.Context, id int64) (*catalog.Card, error) {
	return uc.refreshByGame(ctx, "pokemon", id)
}

func (uc *ImportCardUseCase) DeleteMTG(ctx context.Context, id int64) error {
	return uc.repo.DeleteCard(ctx, id)
}

func (uc *ImportCardUseCase) RefreshMTG(ctx context.Context, id int64) (*catalog.Card, error) {
	return uc.refreshByGame(ctx, "mtg", id)
}

func (uc *ImportCardUseCase) refreshByGame(ctx context.Context, gameCode string, id int64) (*catalog.Card, error) {
	externalID, err := uc.repo.GetExternalID(ctx, id)
	if err != nil {
		return nil, err
	}
	return refreshCard(ctx, uc.search, uc.repo, gameCode, externalID)
}
