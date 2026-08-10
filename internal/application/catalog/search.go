package catalog

import (
	"context"
	"fmt"
	"math"
	"trample-back/internal/domain/catalog"
	"trample-back/internal/ports/out"
)

type SearchScrydex struct {
	scrydex out.ScrydexClient
	trm     out.TRMClient
}

func NewSearchScrydex(scrydex out.ScrydexClient, trm out.TRMClient) *SearchScrydex {
	return &SearchScrydex{scrydex: scrydex, trm: trm}
}

func (uc *SearchScrydex) Search(ctx context.Context, p out.SearchParams) ([]catalog.Card, error) {
	if p.GameCode == "" || p.Name == "" {
		return nil, fmt.Errorf("game_code y name son requeridos")
	}
	cards, err := uc.scrydex.SearchCards(ctx, p)
	if err != nil {
		return nil, err
	}
	return uc.applyTRM(ctx, cards)
}

func (uc *SearchScrydex) FetchOne(ctx context.Context, gameCode, externalID string, variants []string) (*catalog.Card, error) {
	if gameCode == "" || externalID == "" {
		return nil, fmt.Errorf("game_code y external_id son requeridos")
	}
	card, err := uc.scrydex.FetchCard(ctx, gameCode, externalID, variants)
	if err != nil {
		return nil, err
	}
	cards, err := uc.applyTRM(ctx, []catalog.Card{*card})
	if err != nil {
		return nil, err
	}
	return &cards[0], nil
}

func (uc *SearchScrydex) applyTRM(ctx context.Context, cards []catalog.Card) ([]catalog.Card, error) {
	trm, err := uc.trm.GetRate(ctx)
	if err != nil {
		return nil, fmt.Errorf("obtener TRM: %w", err)
	}
	for i := range cards {
		for j := range cards[i].Variants {
			p := cards[i].Variants[j].NMPrice
			if p == nil {
				continue
			}
			p.MarketCOP = int64(math.Round(p.MarketUSD * trm))
			p.LowCOP = int64(math.Round(p.LowUSD * trm))
			p.TRMUsed = trm
		}
	}
	return cards, nil
}
