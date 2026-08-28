package catalog

import (
	"context"
	"fmt"
	"strings"
	"trample-back/internal/domain/catalog"
	"trample-back/internal/ports/out"
)

type SyncExpansionsUseCase struct {
	scrydex out.ScrydexClient
	repo    out.ExpansionRepository
}

func NewSyncExpansionsUseCase(scrydex out.ScrydexClient, repo out.ExpansionRepository) *SyncExpansionsUseCase {
	return &SyncExpansionsUseCase{scrydex: scrydex, repo: repo}
}

func (uc *SyncExpansionsUseCase) SyncPokemon(ctx context.Context) (int, error) {
	expansions, err := uc.scrydex.FetchExpansions(ctx, "pokemon")
	if err != nil {
		return 0, fmt.Errorf("traer expansiones de scrydex: %w", err)
	}
	expansions = filterPokemonExpansions(expansions)
	if err := uc.repo.SyncExpansions(ctx, "pokemon", expansions); err != nil {
		return 0, fmt.Errorf("guardar expansiones: %w", err)
	}
	return len(expansions), nil
}

// filterPokemonExpansions descarta los sets que no son del TCG físico en inglés:
//   - external_id con sufijo "_ja" (sets japoneses)
//   - external_id que empiezan con "tcgp-" (Pokémon TCG Pocket)
func filterPokemonExpansions(expansions []catalog.Expansion) []catalog.Expansion {
	filtered := make([]catalog.Expansion, 0, len(expansions))
	for _, e := range expansions {
		if strings.HasSuffix(e.ExternalID, "_ja") || strings.HasPrefix(e.ExternalID, "tcgp-") {
			continue
		}
		filtered = append(filtered, e)
	}
	return filtered
}

func (uc *SyncExpansionsUseCase) ListPokemon(ctx context.Context) ([]catalog.Expansion, error) {
	return uc.repo.ListExpansions(ctx, "pokemon")
}

func (uc *SyncExpansionsUseCase) ListByGameID(ctx context.Context, gameID int64) ([]catalog.Expansion, error) {
	return uc.repo.ListByGameID(ctx, gameID)
}

func (uc *SyncExpansionsUseCase) ListAll(ctx context.Context) ([]catalog.Expansion, error) {
	return uc.repo.ListAll(ctx)
}

func (uc *SyncExpansionsUseCase) SyncMTG(ctx context.Context) (int, error) {
	expansions, err := uc.scrydex.FetchExpansions(ctx, "mtg")
	if err != nil {
		return 0, fmt.Errorf("traer expansiones de scrydex: %w", err)
	}
	if err := uc.repo.SyncExpansions(ctx, "mtg", expansions); err != nil {
		return 0, fmt.Errorf("guardar expansiones: %w", err)
	}
	return len(expansions), nil
}

func (uc *SyncExpansionsUseCase) ListMTG(ctx context.Context) ([]catalog.Expansion, error) {
	return uc.repo.ListExpansions(ctx, "mtg")
}

func (uc *SyncExpansionsUseCase) SyncRiftbound(ctx context.Context) (int, error) {
	expansions, err := uc.scrydex.FetchExpansions(ctx, "riftbound")
	if err != nil {
		return 0, fmt.Errorf("traer expansiones de scrydex: %w", err)
	}
	if err := uc.repo.SyncExpansions(ctx, "riftbound", expansions); err != nil {
		return 0, fmt.Errorf("guardar expansiones: %w", err)
	}
	return len(expansions), nil
}

func (uc *SyncExpansionsUseCase) ListRiftbound(ctx context.Context) ([]catalog.Expansion, error) {
	return uc.repo.ListExpansions(ctx, "riftbound")
}
