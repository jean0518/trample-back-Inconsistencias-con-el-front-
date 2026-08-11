package scrydex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
	"trample-back/internal/domain/catalog"
	"trample-back/internal/ports/out"
)

const baseURL = "https://api.scrydex.com"

type Client struct {
	apiKey string
	teamID string
	http   *http.Client
}

func NewClient(apiKey, teamID string) *Client {
	return &Client{
		apiKey: apiKey,
		teamID: teamID,
		http:   &http.Client{Timeout: 10 * time.Second},
	}
}

// --- Scrydex JSON structs (reflejan el JSON real de la API) ---

type scrydexEnvelope struct {
	Data []scrydexCard `json:"data"`
}

type scrydexSingleEnvelope struct {
	Data scrydexCard `json:"data"`
}

type scrydexCard struct {
	ID                  string           `json:"id"`
	Name                string           `json:"name"`
	Supertype           string           `json:"supertype"`
	Subtypes            []string         `json:"subtypes"`
	Types               []string         `json:"types"`
	HP                  string           `json:"hp"`
	Number              string           `json:"number"`
	PrintedNumber       string           `json:"printed_number"`
	Rarity              string           `json:"rarity"`
	RarityCode          string           `json:"rarity_code"`
	Artist              string           `json:"artist"`
	FlavorText          string           `json:"flavor_text"`
	Language            string           `json:"language"`
	LanguageCode        string           `json:"language_code"`
	ExpansionSortOrder  int              `json:"expansion_sort_order"`
	NationalPokedexNums []int            `json:"national_pokedex_numbers"`
	EvolvesFrom         []string         `json:"evolves_from"`
	Abilities           json.RawMessage  `json:"abilities"`
	Attacks             json.RawMessage  `json:"attacks"`
	Weaknesses          json.RawMessage  `json:"weaknesses"`
	Resistances         json.RawMessage  `json:"resistances"`
	RetreatCost         []string         `json:"retreat_cost"`
	Images              []scrydexImage   `json:"images"`
	Expansion           scrydexExp       `json:"expansion"`
	Variants            []scrydexVariant `json:"variants"`
}

type scrydexExp struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Series       string `json:"series"`
	Code         string `json:"code"`
	Total        int    `json:"total"`
	PrintedTotal int    `json:"printed_total"`
	Language     string `json:"language"`
	LanguageCode string  `json:"language_code"`
	ReleaseDate  string `json:"release_date"`
	Logo         string `json:"logo"`
	Symbol       string `json:"symbol"`
}

type scrydexImage struct {
	Type   string `json:"type"`
	Small  string `json:"small"`
	Medium string `json:"medium"`
	Large  string `json:"large"`
}

type scrydexVariant struct {
	Name         string              `json:"name"`
	Origin       string              `json:"origin"`
	Images       []scrydexImage      `json:"images"`
	Marketplaces []scrydexMarketplace `json:"marketplaces"`
	Prices       []scrydexPrice      `json:"prices"`
}

type scrydexMarketplace struct {
	Name        string `json:"name"`
	ProductID   string `json:"product_id"`
	PurchaseURL string `json:"purchase_url"`
}

type scrydexPrice struct {
	Condition string   `json:"condition"`
	Market    float64  `json:"market"`
	Low       *float64 `json:"low"`
	Currency  string   `json:"currency"`
}

// --- Métodos públicos ---

func (c *Client) SearchCards(ctx context.Context, p out.SearchParams) ([]catalog.Card, error) {
	q := buildQuery(p.Name, p.ExpansionCode, p.Rarity)
	endpoint := fmt.Sprintf(
		"%s/%s/v1/cards?q=%s&include=prices,images,variants&page_size=20",
		baseURL, p.GameCode, url.QueryEscape(q),
	)

	slog.Info("scrydex request", slog.String("url", endpoint), slog.Any("variant_filter", p.Variants))

	var envelope scrydexEnvelope
	if err := c.get(ctx, endpoint, &envelope); err != nil {
		return nil, err
	}

	cards := make([]catalog.Card, 0, len(envelope.Data))
	for _, raw := range envelope.Data {
		card := toCard(raw)
		card.Variants = filterVariants(card.Variants, p.Variants)
		cards = append(cards, card)
	}
	return cards, nil
}

func (c *Client) FetchExpansions(ctx context.Context, gameCode string) ([]catalog.Expansion, error) {
	const pageSize = 100

	var envelope struct {
		Data       []scrydexExp `json:"data"`
		TotalCount int          `json:"total_count"`
	}

	var all []catalog.Expansion
	page := 1

	for {
		endpoint := fmt.Sprintf("%s/%s/v1/expansions?page_size=%d&page=%d", baseURL, gameCode, pageSize, page)
		slog.Info("scrydex expansions request", slog.String("url", endpoint))

		if err := c.get(ctx, endpoint, &envelope); err != nil {
			return nil, err
		}

		for _, e := range envelope.Data {
			all = append(all, catalog.Expansion{
				ExternalID:  e.ID,
				Name:        e.Name,
				Series:      e.Series,
				Code:        e.Code,
				Total:       e.Total,
				ReleaseDate: e.ReleaseDate,
				Logo:        e.Logo,
				Symbol:      e.Symbol,
			})
		}

		if len(all) >= envelope.TotalCount || len(envelope.Data) < pageSize {
			break
		}
		page++
	}

	return all, nil
}

func (c *Client) FetchCard(ctx context.Context, gameCode, externalID string, variants []string) (*catalog.Card, error) {
	endpoint := fmt.Sprintf(
		"%s/%s/v1/cards/%s?include=prices,images,variants",
		baseURL, gameCode, externalID,
	)

	slog.Info("scrydex request", slog.String("url", endpoint))

	var envelope scrydexSingleEnvelope
	if err := c.get(ctx, endpoint, &envelope); err != nil {
		return nil, err
	}

	card := toCard(envelope.Data)
	card.Variants = filterVariants(card.Variants, variants)
	return &card, nil
}

func filterVariants(variants []catalog.Variant, filter []string) []catalog.Variant {
	if len(filter) == 0 {
		return variants
	}
	allowed := make(map[string]bool, len(filter))
	for _, f := range filter {
		allowed[f] = true
	}
	result := make([]catalog.Variant, 0)
	for _, v := range variants {
		if allowed[v.Name] {
			result = append(result, v)
		}
	}
	return result
}

// --- Mapeo ---

func toCard(s scrydexCard) catalog.Card {
	return catalog.Card{
		ExternalID:          s.ID,
		Name:                s.Name,
		Supertype:           s.Supertype,
		Subtypes:            s.Subtypes,
		Types:               s.Types,
		HP:                  s.HP,
		Number:              s.Number,
		PrintedNumber:       s.PrintedNumber,
		Rarity:              s.Rarity,
		RarityCode:          s.RarityCode,
		Artist:              s.Artist,
		FlavorText:          s.FlavorText,
		Language:            s.Language,
		LanguageCode:        s.LanguageCode,
		ExpansionSortOrder:  s.ExpansionSortOrder,
		NationalPokedexNums: s.NationalPokedexNums,
		EvolvesFrom:         s.EvolvesFrom,
		Abilities:           s.Abilities,
		Attacks:             s.Attacks,
		Weaknesses:          s.Weaknesses,
		Resistances:         s.Resistances,
		RetreatCost:         s.RetreatCost,
		Expansion: catalog.Expansion{
			ExternalID:   s.Expansion.ID,
			Name:         s.Expansion.Name,
			Series:       s.Expansion.Series,
			Code:         s.Expansion.Code,
			Total:        s.Expansion.Total,
			PrintedTotal: s.Expansion.PrintedTotal,
			Language:     s.Expansion.Language,
			LanguageCode: s.Expansion.LanguageCode,
			ReleaseDate:  s.Expansion.ReleaseDate,
			Logo:         s.Expansion.Logo,
			Symbol:       s.Expansion.Symbol,
		},
		Images:   toImages(s.Images),
		Variants: toVariants(s.Variants),
	}
}

func toImages(raw []scrydexImage) []catalog.Image {
	imgs := make([]catalog.Image, 0, len(raw))
	for _, img := range raw {
		imgs = append(imgs, catalog.Image{
			Type:   img.Type,
			Small:  img.Small,
			Medium: img.Medium,
			Large:  img.Large,
		})
	}
	return imgs
}

var excludedVariants = map[string]bool{
	"jumbo": true,
	"metal": true,
}

func toVariants(raw []scrydexVariant) []catalog.Variant {
	variants := make([]catalog.Variant, 0, len(raw))
	for _, v := range raw {
		if excludedVariants[v.Name] {
			continue
		}
		var nmPrice *catalog.Price
		for _, p := range v.Prices {
			if p.Condition == "NM" {
				low := 0.0
				if p.Low != nil {
					low = *p.Low
				}
				nmPrice = &catalog.Price{
					MarketUSD: p.Market,
					LowUSD:    low,
				}
				break
			}
		}

		marketplaces := make([]catalog.Marketplace, 0, len(v.Marketplaces))
		for _, m := range v.Marketplaces {
			marketplaces = append(marketplaces, catalog.Marketplace{
				Name:        m.Name,
				ProductID:   m.ProductID,
				PurchaseURL: m.PurchaseURL,
			})
		}

		variants = append(variants, catalog.Variant{
			Name:         v.Name,
			Origin:       v.Origin,
			Images:       toImages(v.Images),
			Marketplaces: marketplaces,
			NMPrice:      nmPrice,
		})
	}
	return variants
}

// --- Query builder ---

func buildQuery(name, expansionCode, rarity string) string {
	var parts []string
	if name != "" {
		parts = append(parts, "name:"+quote(name))
	}
	if expansionCode != "" {
		parts = append(parts, "expansion.id:"+quote(expansionCode))
	}
	if rarity != "" {
		parts = append(parts, "rarity:"+quote(rarity))
	}
	// Excluye cartas de Pokémon TCG Pocket (mobile, distinto al TCG físico)
	parts = append(parts, `-expansion.series:"Pokémon Pocket"`)
	return strings.Join(parts, " ")
}

func quote(v string) string {
	if strings.Contains(v, " ") {
		return `"` + v + `"`
	}
	return v
}

// --- HTTP helper ---

func (c *Client) get(ctx context.Context, endpoint string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", c.apiKey)
	req.Header.Set("X-Team-ID", c.teamID)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("scrydex: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("scrydex: HTTP %d para %s", resp.StatusCode, endpoint)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	return json.NewDecoder(bytes.NewReader(body)).Decode(out)
}
