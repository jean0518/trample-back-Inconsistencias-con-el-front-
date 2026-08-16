package catalog

import (
	"context"
	"fmt"
	"trample-back/internal/domain/catalog"
	"trample-back/internal/ports/out"
)

type ImportCardUseCase struct {
	search        *SearchScrydex
	repo          out.CardRepository
	expansionRepo out.ExpansionRepository
}

func NewImportCardUseCase(search *SearchScrydex, repo out.CardRepository, expansionRepo out.ExpansionRepository) *ImportCardUseCase {
	return &ImportCardUseCase{search: search, repo: repo, expansionRepo: expansionRepo}
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

func (uc *ImportCardUseCase) ImportPokemon(ctx context.Context, name, expansionName, rarity string) ([]catalog.Card, error) {
	return uc.importByGame(ctx, "pokemon", name, expansionName, rarity)
}

func (uc *ImportCardUseCase) DeletePokemon(ctx context.Context, id int64) error {
	return uc.repo.DeleteCard(ctx, id)
}

func (uc *ImportCardUseCase) RefreshPokemon(ctx context.Context, id int64) (*catalog.Card, error) {
	return uc.refreshByGame(ctx, "pokemon", id)
}

func (uc *ImportCardUseCase) ImportMTG(ctx context.Context, name, expansionName, rarity string) ([]catalog.Card, error) {
	return uc.importByGame(ctx, "mtg", name, expansionName, rarity)
}

func (uc *ImportCardUseCase) DeleteMTG(ctx context.Context, id int64) error {
	return uc.repo.DeleteCard(ctx, id)
}

func (uc *ImportCardUseCase) RefreshMTG(ctx context.Context, id int64) (*catalog.Card, error) {
	return uc.refreshByGame(ctx, "mtg", id)
}

func (uc *ImportCardUseCase) importByGame(ctx context.Context, gameCode, name, expansionName, rarity string) ([]catalog.Card, error) {
	var expansionCode string
	if expansionName != "" {
		expansion, err := uc.expansionRepo.FindByName(ctx, gameCode, expansionName)
		if err != nil {
			return nil, fmt.Errorf("expansión %q no encontrada: asegúrate de haber sincronizado las expansiones primero", expansionName)
		}
		expansionCode = expansion.ExternalID
	}

	cards, err := uc.search.Search(ctx, out.SearchParams{
		GameCode:      gameCode,
		Name:          name,
		ExpansionCode: expansionCode,
		Rarity:        rarity,
	})
	if err != nil {
		return nil, fmt.Errorf("buscar en scrydex: %w", err)
	}

	for _, card := range cards {
		if err := uc.repo.SyncCard(ctx, gameCode, card); err != nil {
			return nil, fmt.Errorf("guardar carta %q: %w", card.Name, err)
		}
	}
	return cards, nil
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
