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
	Supertype     string   `json:"supertype"`
	LanguageCode  string   `json:"language_code"`
}

type ScrydexClient interface {
	SearchCards(ctx context.Context, p SearchParams) ([]catalog.Card, error)
	FetchCard(ctx context.Context, gameCode, externalID string, variants []string) (*catalog.Card, error)
	FetchExpansions(ctx context.Context, gameCode string) ([]catalog.Expansion, error)
	// FetchExpansionCards devuelve todas las cartas de una expansión con sus
	// precios. Hace las páginas necesarias internamente (page_size=100).
	FetchExpansionCards(ctx context.Context, gameCode, expansionExternalID string) ([]catalog.Card, error)
}
