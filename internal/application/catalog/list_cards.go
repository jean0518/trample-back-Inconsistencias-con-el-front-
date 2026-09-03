package catalog

import (
	"context"
	"trample-back/internal/domain/catalog"
	"trample-back/internal/ports/out"
)

type ListCardsUseCase struct {
	repo      out.CardRepository
	refresher *PriceRefresher
}

// refresher es opcional: si es nil, listar cartas no dispara refrescos
// perezosos de precio (útil en tests).
func NewListCardsUseCase(repo out.CardRepository, refresher *PriceRefresher) *ListCardsUseCase {
	return &ListCardsUseCase{repo: repo, refresher: refresher}
}

type ListCardsResult struct {
	Cards    []catalog.CardSummary
	Total    int
	Page     int
	PageSize int
}

func (uc *ListCardsUseCase) List(ctx context.Context, p out.ListCardsParams) (ListCardsResult, error) {
	if p.Page <= 0 {
		p.Page = 1
	}
	if p.PageSize <= 0 {
		p.PageSize = 20
	}
	if p.PageSize > 100 {
		p.PageSize = 100
	}

	cards, total, err := uc.repo.ListCards(ctx, p)
	if err != nil {
		return ListCardsResult{}, err
	}
	if cards == nil {
		cards = []catalog.CardSummary{}
	}
	if uc.refresher != nil {
		uc.refresher.TriggerLazy(ctx)
	}
	return ListCardsResult{
		Cards:    cards,
		Total:    total,
		Page:     p.Page,
		PageSize: p.PageSize,
	}, nil
}
