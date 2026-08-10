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
	Language            string          `json:"language,omitempty"`
	LanguageCode        string          `json:"language_code,omitempty"`
	ExpansionSortOrder  int             `json:"expansion_sort_order,omitempty"`
	NationalPokedexNums []int           `json:"national_pokedex_numbers,omitempty"`
	EvolvesFrom         []string        `json:"evolves_from,omitempty"`
	Abilities           json.RawMessage `json:"abilities,omitempty"`
	Attacks             json.RawMessage `json:"attacks,omitempty"`
	Weaknesses          json.RawMessage `json:"weaknesses,omitempty"`
	Resistances         json.RawMessage `json:"resistances,omitempty"`
	RetreatCost         []string        `json:"retreat_cost,omitempty"`
	Expansion           Expansion       `json:"expansion"`
	Images              []Image         `json:"images"`
	Variants            []Variant       `json:"variants"`
}

type Expansion struct {
	ExternalID   string `json:"external_id"`
	Name         string `json:"name"`
	Series       string `json:"series,omitempty"`
	Code         string `json:"code,omitempty"`
	Total        int    `json:"total,omitempty"`
	PrintedTotal int    `json:"printed_total,omitempty"`
	Language     string `json:"language,omitempty"`
	LanguageCode string `json:"language_code,omitempty"`
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
	Name        string      `json:"name"`
	Origin      string      `json:"origin,omitempty"`
	Images      []Image     `json:"images"`
	Marketplaces []Marketplace `json:"marketplaces,omitempty"`
	NMPrice     *Price      `json:"nm_price"`
}

type Marketplace struct {
	Name        string `json:"name"`
	ProductID   string `json:"product_id"`
	PurchaseURL string `json:"purchase_url"`
}

type Price struct {
	MarketUSD float64 `json:"market_usd"`
	LowUSD    float64 `json:"low_usd"`
	MarketCOP int64   `json:"market_cop"`
	LowCOP    int64   `json:"low_cop"`
	TRMUsed   float64 `json:"trm_used"`
}
