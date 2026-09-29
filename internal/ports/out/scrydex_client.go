package out

import (
	"context"
	"trample-back/internal/domain/catalog"
)

type SearchParams struct {
	GameCode string `json:"game_code"`
	Name     string `json:"name"`
	// ExternalID acota la búsqueda a una impression exacta (filtro `id:` de
	// Scrydex). Es imprescindible cuando el nombre no es único: "Mew ex" tiene
	// decenas de impresiones con precios muy distintos y buscar solo por nombre
	// devuelve la que Scrydex ordene primero, no la que se pidió.
	//
	// Solo aplica al índice del idioma buscado: las impresiones japonesas
	// tienen su propio ID (sv4pt5_ja-216 en vez de sv4pt5-216), así que un
	// `id:` de la versión inglesa no encuentra la japonesa. Para esos casos el
	// llamador debe resolver por número impreso y rareza, no por ID.
	ExternalID    string   `json:"external_id"`
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
