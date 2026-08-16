package out

import (
	"context"
	"trample-back/internal/domain/catalog"
)

type SearchParams struct {
	GameCode      string   `json:"game_code"`
	Name          string   `json:"name"`
	ExpansionCode string   `json:"expansion_code"`
	Rarity        string   `json:"rarity"`
	Variants      []string `json:"variants"`
	Type          string   `json:"type"`
}

type ScrydexClient interface {
	SearchCards(ctx context.Context, p SearchParams) ([]catalog.Card, error)
	FetchCard(ctx context.Context, gameCode, externalID string, variants []string) (*catalog.Card, error)
	FetchExpansions(ctx context.Context, gameCode string) ([]catalog.Expansion, error)
}
