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

func (uc *ImportCardUseCase) ImportPokemon(ctx context.Context, name, expansionName, rarity string) ([]catalog.Card, error) {
	var expansionCode string

	if expansionName != "" {
		expansion, err := uc.expansionRepo.FindByName(ctx, "pokemon", expansionName)
		if err != nil {
			return nil, fmt.Errorf("expansión %q no encontrada: asegúrate de haber sincronizado las expansiones primero", expansionName)
		}
		expansionCode = expansion.ExternalID
	}

	cards, err := uc.search.Search(ctx, out.SearchParams{
		GameCode:      "pokemon",
		Name:          name,
		ExpansionCode: expansionCode,
		Rarity:        rarity,
	})
	if err != nil {
		return nil, fmt.Errorf("buscar en scrydex: %w", err)
	}

	for _, card := range cards {
		if err := uc.repo.SyncCard(ctx, "pokemon", card); err != nil {
			return nil, fmt.Errorf("guardar carta %q: %w", card.Name, err)
		}
	}

	return cards, nil
}
