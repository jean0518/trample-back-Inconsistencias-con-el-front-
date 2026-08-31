package catalog

import "encoding/json"

type Card struct {
	ExternalID          string          `json:"external_id"`
	Name                string          `json:"name"`
	Supertype           string          `json:"supertype"`
	Subtypes            []string        `json:"subtypes"`
	Types               []string        `json:"types"`
	HP                  string          `json:"hp,omitempty"`
	Number              string          `json:"number"`
	PrintedNumber       string          `json:"printed_number,omitempty"`
	Rarity              string          `json:"rarity"`
	RarityCode          string          `json:"rarity_code,omitempty"`
	Artist              string          `json:"artist,omitempty"`
	FlavorText          string          `json:"flavor_text,omitempty"`
	ExpansionSortOrder  int             `json:"expansion_sort_order,omitempty"`
	NationalPokedexNums []int           `json:"national_pokedex_numbers,omitempty"`
	EvolvesFrom         []string        `json:"evolves_from,omitempty"`
	Abilities           json.RawMessage `json:"abilities,omitempty"   swaggertype:"array,object"`
	Attacks             json.RawMessage `json:"attacks,omitempty"     swaggertype:"array,object"`
	Weaknesses          json.RawMessage `json:"weaknesses,omitempty"  swaggertype:"array,object"`
	Resistances         json.RawMessage `json:"resistances,omitempty" swaggertype:"array,object"`
	RetreatCost         []string        `json:"retreat_cost,omitempty"`
	// MTG
	Colors        []string        `json:"colors,omitempty"`
	ColorIdentity []string        `json:"color_identity,omitempty"`
	ManaCost      string          `json:"mana_cost,omitempty"`
	ManaValue     int             `json:"mana_value,omitempty"`
	Power         string          `json:"power,omitempty"`
	Toughness     string          `json:"toughness,omitempty"`
	TypeLine      string          `json:"type_line,omitempty"`
	Rules         []string        `json:"rules,omitempty"`
	Keywords      []string        `json:"keywords,omitempty"`
	Layout        string          `json:"layout,omitempty"`
	Faces         json.RawMessage `json:"faces,omitempty"   swaggertype:"array,object"`
	Rulings       json.RawMessage `json:"rulings,omitempty" swaggertype:"array,object"`
	Expansion     Expansion       `json:"expansion"`
	Images        []Image         `json:"images"`
	Variants      []Variant       `json:"variants"`
}

type Expansion struct {
	ID           int64  `json:"id,omitempty"`
	GameID       int64  `json:"game_id,omitempty"`
	ExternalID   string `json:"external_id"`
	Name         string `json:"name"`
	Series       string `json:"series,omitempty"`
	Code         string `json:"code,omitempty"`
	Total        int    `json:"total,omitempty"`
	PrintedTotal int    `json:"printed_total,omitempty"`
	ReleaseDate  string `json:"release_date,omitempty"`
	Logo         string `json:"logo,omitempty"`
	Symbol       string `json:"symbol,omitempty"`
}

type Image struct {
	Type   string `json:"type"`
	Small  string `json:"small"`
	Medium string `json:"medium"`
	Large  string `json:"large"`
}

type Variant struct {
	Name         string        `json:"name"`
	Origin       string        `json:"origin,omitempty"`
	Images       []Image       `json:"images"`
	Marketplaces []Marketplace `json:"marketplaces,omitempty"`
	NMPrice      *Price        `json:"nm_price"`
}

type Marketplace struct {
	Name        string `json:"name"`
	ProductID   string `json:"product_id"`
	PurchaseURL string `json:"purchase_url"`
}

type CardSummary struct {
	ID         int64          `json:"id"`
	ExternalID string         `json:"external_id"`
	Name       string         `json:"name"`
	Number     string         `json:"number"`
	Rarity     string         `json:"rarity"`
	GameCode   string         `json:"game_code"`
	Expansion  ExpansionBrief `json:"expansion"`
	Image      ImageBrief     `json:"image"`
	Variants   []VariantBrief `json:"variants"`
	// Stock agregado de listings activos con cantidad > 0. El catálogo
	// público solo muestra cartas con stock disponible.
	Stock     int    `json:"stock"`
	OwnerName string `json:"owner_name"`
	Language  string `json:"language"`
	// Idiomas disponibles para la carta con su stock. Cuando hay más de uno,
	// el cliente puede elegir el idioma antes de comprar.
	Languages []CardLanguageBrief `json:"languages"`
}

type VariantBrief struct {
	Name     string  `json:"name"`
	PriceUSD float64 `json:"price_usd"`
	PriceCOP int64   `json:"price_cop"`
}

// Idiomas en los que existe una carta en el inventario, con su stock.
type CardLanguageBrief struct {
	Name  string `json:"name"`
	Stock int    `json:"stock"`
}

type ExpansionBrief struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Code      string `json:"code"`
	LogoURL   string `json:"logo_url"`
	SymbolURL string `json:"symbol_url"`
}

type ImageBrief struct {
	Small  string `json:"small"`
	Medium string `json:"medium"`
	Large  string `json:"large"`
}

type Price struct {
	MarketUSD float64 `json:"market_usd"`
	LowUSD    float64 `json:"low_usd"`
	MarketCOP int64   `json:"market_cop"`
	LowCOP    int64   `json:"low_cop"`
	TRMUsed   float64 `json:"trm_used"`
}
