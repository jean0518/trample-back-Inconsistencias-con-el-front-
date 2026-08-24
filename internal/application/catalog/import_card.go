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

// ImportGroup es un lote de cartas elegidas dentro de una búsqueda previa.
// La selección temporal del front puede acumular grupos de búsquedas distintas
// (otros juegos u otras expansiones) antes de importar todo junto.
type ImportGroup struct {
	SearchID    string   `json:"search_id"`
	ExternalIDs []string `json:"external_ids"`
}

func NewImportCardUseCase(search *SearchScrydex, repo out.CardRepository) *ImportCardUseCase {
	return &ImportCardUseCase{search: search, repo: repo}
}

// ImportByGroups persiste las cartas seleccionadas de una o varias búsquedas
// previas. Los resultados se leen de la caché del servidor, así importar no
// vuelve a consumir llamadas de la API de Scrydex. Las cartas repetidas entre
// grupos se deduplican por juego + ID externo.
func (uc *ImportCardUseCase) ImportByGroups(ctx context.Context, groups []ImportGroup) ([]catalog.Card, error) {
	if len(groups) == 0 {
		return nil, fmt.Errorf("groups es requerido")
	}

	seen := make(map[string]bool)
	var imported []catalog.Card

	for _, g := range groups {
		if g.SearchID == "" || len(g.ExternalIDs) == 0 {
			return nil, fmt.Errorf("cada grupo requiere search_id y external_ids")
		}
		cached, ok := uc.search.get(g.SearchID)
		if !ok {
			return nil, fmt.Errorf("la búsqueda %s expiró, repetila e intentá de nuevo", g.SearchID)
		}

		wanted := make(map[string]bool, len(g.ExternalIDs))
		for _, id := range g.ExternalIDs {
			wanted[id] = true
		}

		for _, card := range cached.Cards {
			if !wanted[card.ExternalID] {
				continue
			}
			key := cached.GameCode + ":" + card.ExternalID
			if seen[key] {
				continue
			}
			seen[key] = true
			if err := uc.repo.SyncCard(ctx, cached.GameCode, card); err != nil {
				return nil, fmt.Errorf("guardar carta %q: %w", card.Name, err)
			}
			imported = append(imported, card)
		}
	}

	if len(imported) == 0 {
		return nil, fmt.Errorf("ninguna de las cartas seleccionadas está en los resultados de las búsquedas")
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
