package catalog

import (
	"context"
	"fmt"
	"trample-back/internal/domain/catalog"
	"trample-back/internal/ports/out"
)

type ImportCardUseCase struct {
	search *SearchScrydex
	repo   out.CardRepository
}

func NewImportCardUseCase(search *SearchScrydex, repo out.CardRepository) *ImportCardUseCase {
	return &ImportCardUseCase{search: search, repo: repo}
}

// ImportBySearch persiste las cartas seleccionadas de una búsqueda previa.
// Los resultados se leen de la caché del servidor, así importar no vuelve a
// consumir llamadas de la API de Scrydex.
func (uc *ImportCardUseCase) ImportBySearch(ctx context.Context, searchID string, externalIDs []string) ([]catalog.Card, error) {
	cached, ok := uc.search.get(searchID)
	if !ok {
		return nil, fmt.Errorf("la búsqueda expiró, repetí la búsqueda")
	}
	if len(externalIDs) == 0 {
		return nil, fmt.Errorf("external_ids es requerido")
	}

	wanted := make(map[string]bool, len(externalIDs))
	for _, id := range externalIDs {
		wanted[id] = true
	}

	var imported []catalog.Card
	for _, card := range cached.Cards {
		if !wanted[card.ExternalID] {
			continue
		}
		if err := uc.repo.SyncCard(ctx, cached.GameCode, card); err != nil {
			return nil, fmt.Errorf("guardar carta %q: %w", card.Name, err)
		}
		imported = append(imported, card)
	}

	if len(imported) == 0 {
		return nil, fmt.Errorf("ninguna de las cartas seleccionadas está en los resultados de la búsqueda")
	}
	return imported, nil
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
	card, err := uc.search.FetchOne(ctx, gameCode, externalID, nil)
	if err != nil {
		return nil, fmt.Errorf("re-fetch de scrydex: %w", err)
	}
	if err := uc.repo.SyncCard(ctx, gameCode, *card); err != nil {
		return nil, fmt.Errorf("actualizar carta: %w", err)
	}
	return card, nil
}
