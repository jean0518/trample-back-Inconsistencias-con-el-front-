package catalog

import (
	"context"
	"fmt"
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
	if err := uc.repo.SyncExpansions(ctx, "pokemon", expansions); err != nil {
		return 0, fmt.Errorf("guardar expansiones: %w", err)
	}
	return len(expansions), nil
}

func (uc *SyncExpansionsUseCase) ListPokemon(ctx context.Context) ([]catalog.Expansion, error) {
	return uc.repo.ListExpansions(ctx, "pokemon")
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
