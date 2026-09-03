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
	"unicode"
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

// stringOrSlice deserializes a JSON field that may be either a string or []string.
type stringOrSlice []string

func (s *stringOrSlice) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	if data[0] == '"' {
		var str string
		if err := json.Unmarshal(data, &str); err != nil {
			return err
		}
		*s = []string{str}
		return nil
	}
	var arr []string
	if err := json.Unmarshal(data, &arr); err != nil {
		return err
	}
	*s = arr
	return nil
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
	Artist              string          `json:"artist"`
	FlavorText          string          `json:"flavor_text"`
	ExpansionSortOrder  int              `json:"expansion_sort_order"`
	NationalPokedexNums []int            `json:"national_pokedex_numbers"`
	EvolvesFrom         stringOrSlice    `json:"evolves_from"`
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
	Name         string               `json:"name"`
	Origin       string               `json:"origin"`
	Images       []scrydexImage       `json:"images"`
	Marketplaces []scrydexMarketplace `json:"marketplaces"`
	Prices       []scrydexPrice       `json:"prices"`
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

// --- Structs MTG ---

type scrydexMTGEnvelope struct {
	Data []scrydexMTGCard `json:"data"`
}

type scrydexMTGSingleEnvelope struct {
	Data scrydexMTGCard `json:"data"`
}

type scrydexMTGCard struct {
	ID             string             `json:"id"`
	Name           string             `json:"name"`
	Supertypes     []string           `json:"supertypes"`
	Subtypes       []string           `json:"subtypes"`
	Types          []string           `json:"types"`
	TypeLine       string             `json:"type"`
	Number         string             `json:"number"`
	ColorIdentity  []string           `json:"color_identity"`
	Colors         []string           `json:"colors"`
	ManaCost       string             `json:"mana_cost"`
	ManaValue      int                `json:"mana_value"`
	Power          string             `json:"power"`
	Toughness      string             `json:"toughness"`
	Rules          []string           `json:"rules"`
	Rarity         string             `json:"rarity"`
	Artist         string             `json:"artist"`
	Keywords       []string           `json:"keywords"`
	Layout         string             `json:"layout"`
	Faces          json.RawMessage    `json:"faces"`
	Images         []scrydexImage     `json:"images"`
	Variants       []scrydexMTGVariant `json:"variants"`
	Rulings        []scrydexMTGRuling `json:"rulings"`
	Expansion      scrydexMTGExp      `json:"expansion"`
	Language       string             `json:"language"`
	LanguageCode   string             `json:"language_code"`
}

type scrydexMTGVariant struct {
	Name        string         `json:"name"`
	Images      []scrydexImage `json:"images"`
	BorderColor string         `json:"border_color"`
	Prices      []scrydexPrice `json:"prices"`
}

type scrydexMTGRuling struct {
	Date string `json:"date"`
	Text string `json:"text"`
}

type scrydexMTGExp struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Code        string `json:"code"`
	Block       string `json:"block"`
	Total       int    `json:"total"`
	ReleaseDate string `json:"release_date"`
	Logo        string `json:"logo"`
	Symbol      string `json:"symbol"`
}

// --- Métodos públicos ---

func (c *Client) SearchCards(ctx context.Context, p out.SearchParams) ([]catalog.Card, error) {
	q := buildQuery(p.GameCode, p.Name, p.ExpansionCode, p.Rarity, p.Type, p.Supertype)
	cards, err := c.searchOnce(ctx, p.GameCode, q, p.Variants)
	if err != nil {
		return nil, err
	}
	// Reintento con el nombre reducido a letras y dígitos: cubre entradas
	// cuyo signo de puntuación no existe en el nombre indexado (p. ej. un
	// punto final en "pikachu."). Si la búsqueda conservada ya arrojó
	// resultados (o la query alternativa es idéntica) no se vuelve a consultar.
	if len(cards) == 0 {
		fallback := buildQuery(p.GameCode, stripNameSymbols(p.Name), p.ExpansionCode, p.Rarity, p.Type, p.Supertype)
		if fallback != q {
			slog.Info("scrydex retry sin simbolos", slog.String("query", fallback))
			cards, err = c.searchOnce(ctx, p.GameCode, fallback, p.Variants)
			if err != nil {
				return nil, err
			}
		}
	}
	if p.GameCode == "pokemon" {
		cards = filterPocketCards(cards)
	}
	return cards, nil
}

// filterPocketCards descarta cartas cuya expansión pertenece a Pokémon TCG
// Pocket (identificadas por el prefijo "tcgp-" en el ID de expansión).
func filterPocketCards(cards []catalog.Card) []catalog.Card {
	out := cards[:0]
	for _, c := range cards {
		if !strings.HasPrefix(c.Expansion.ExternalID, "tcgp-") {
			out = append(out, c)
		}
	}
	return out
}

func (c *Client) searchOnce(ctx context.Context, gameCode, q string, variants []string) ([]catalog.Card, error) {
	endpoint := fmt.Sprintf(
		"%s%s/cards?q=%s&include=prices,images,variants&page_size=20",
		baseURL, cardsPath(gameCode), url.QueryEscape(q),
	)

	slog.Info("scrydex request", slog.String("url", endpoint), slog.Any("variant_filter", variants))

	if gameCode == "mtg" {
		var envelope scrydexMTGEnvelope
		if err := c.get(ctx, endpoint, &envelope); err != nil {
			return nil, err
		}
		cards := make([]catalog.Card, 0, len(envelope.Data))
		for _, raw := range envelope.Data {
			card := toMTGCard(raw)
			card.Variants = filterVariants(card.Variants, variants)
			cards = append(cards, card)
		}
		return cards, nil
	}

	var envelope scrydexEnvelope
	if err := c.get(ctx, endpoint, &envelope); err != nil {
		return nil, err
	}
	cards := make([]catalog.Card, 0, len(envelope.Data))
	for _, raw := range envelope.Data {
		card := toCard(raw)
		card.Variants = filterVariants(card.Variants, variants)
		cards = append(cards, card)
	}
	return cards, nil
}

// expansionsPath devuelve el segmento de ruta que usa Scrydex para cada juego.
// MTG usa "magicthegathering" en el path de expansiones aunque las cartas usen "mtg".
func expansionsPath(gameCode string) string {
	if gameCode == "mtg" {
		return "magicthegathering"
	}
	return gameCode
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
		endpoint := fmt.Sprintf("%s/%s/v1/expansions?page_size=%d&page=%d", baseURL, expansionsPath(gameCode), pageSize, page)
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
		"%s%s/cards/%s?include=prices,images,variants",
		baseURL, cardsPath(gameCode), externalID,
	)

	slog.Info("scrydex request", slog.String("url", endpoint))

	if gameCode == "mtg" {
		var envelope scrydexMTGSingleEnvelope
		if err := c.get(ctx, endpoint, &envelope); err != nil {
			return nil, err
		}
		card := toMTGCard(envelope.Data)
		card.Variants = filterVariants(card.Variants, variants)
		return &card, nil
	}

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

func toMTGCard(s scrydexMTGCard) catalog.Card {
	rulingsJSON, _ := json.Marshal(s.Rulings)
	return catalog.Card{
		ExternalID:    s.ID,
		Name:          s.Name,
		Subtypes:      s.Subtypes,
		Types:         s.Types,
		TypeLine:      s.TypeLine,
		Number:        s.Number,
		Rarity:        s.Rarity,
		Artist:        s.Artist,
		Colors:        s.Colors,
		ColorIdentity: s.ColorIdentity,
		ManaCost:      s.ManaCost,
		ManaValue:     s.ManaValue,
		Power:         s.Power,
		Toughness:     s.Toughness,
		Rules:         s.Rules,
		Keywords:      s.Keywords,
		Layout:        s.Layout,
		Faces:         s.Faces,
		Rulings:       rulingsJSON,
		Expansion: catalog.Expansion{
			ExternalID:  s.Expansion.ID,
			Name:        s.Expansion.Name,
			Code:        s.Expansion.Code,
			Series:      s.Expansion.Block,
			Total:       s.Expansion.Total,
			ReleaseDate: s.Expansion.ReleaseDate,
			Logo:        s.Expansion.Logo,
			Symbol:      s.Expansion.Symbol,
		},
		Images:   toImages(s.Images),
		Variants: toMTGVariants(s.Variants),
	}
}

func toMTGVariants(raw []scrydexMTGVariant) []catalog.Variant {
	variants := make([]catalog.Variant, 0, len(raw))
	for _, v := range raw {
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
		variants = append(variants, catalog.Variant{
			Name:    v.Name,
			Images:  toImages(v.Images),
			NMPrice: nmPrice,
		})
	}
	return variants
}

// --- Query builder ---

func buildQuery(gameCode, name, expansionCode, rarity, cardType, supertype string) string {
	var parts []string
	if clause := buildNameClause(name); clause != "" {
		parts = append(parts, clause)
	}
	if expansionCode != "" {
		parts = append(parts, "expansion.id:"+quote(expansionCode))
	}
	if rarity != "" {
		parts = append(parts, "rarity:"+quote(rarity))
	}
	if cardType != "" {
		parts = append(parts, "types:"+quote(cardType))
	}
	// Supertipo (solo Pokémon): Pokémon, Trainer o Energy.
	if supertype != "" && gameCode == "pokemon" {
		parts = append(parts, "supertype:"+`"`+supertype+`"`)
	}
	// Excluye cartas de Pokémon TCG Pocket (mobile, distinto al TCG físico).
	// Solo aplica a Pokémon: para mtg/riftbound el filtro no tiene sentido.
	if gameCode == "pokemon" {
		parts = append(parts, `-expansion.series:"Pokémon Pocket"`)
	}
	return strings.Join(parts, " ")
}

// cardsPath construye la ruta base de la API.
// MTG usa "magicthegathering" en todos sus endpoints de Scrydex.
func cardsPath(gameCode string) string {
	if gameCode == "mtg" {
		return "/magicthegathering/v1"
	}
	return fmt.Sprintf("/%s/v1", gameCode)
}

func quote(v string) string {
	if strings.Contains(v, " ") {
		return `"` + v + `"`
	}
	return v
}

// buildNameClause arma el filtro de nombre para la query de Scrydex.
//   - Un solo término usa wildcard de prefijo:  name:pikachu*
//   - Varias palabras se buscan como frase exacta:  name:"rare candy"
//
// La puntuación que forma parte del nombre se conserva (véase
// sanitizeNameTerm). Un comodín después de las comillas (name:"..."*) no es
// sintaxis válida para el parser de Scrydex y responde HTTP 400.
func buildNameClause(raw string) string {
	tokens := fieldsWithLetters(sanitizeNameTerm(raw))
	if len(tokens) == 0 {
		return ""
	}
	if len(tokens) == 1 {
		return "name:" + tokens[0] + "*"
	}
	return `name:"` + strings.Join(tokens, " ") + `"`
}

// sanitizeNameTerm conserva los caracteres que el índice de Scrydex mantiene
// dentro de los nombres —apóstrofes, guiones internos, puntos y "&" forman
// parte de términos como "Rocket's", "Charizard-GX", "Mr. Mime" o
// "Pikachu & Zekrom"— y elimina únicamente los que tienen significado
// sintáctico para su parser de queries (comillas, dos puntos, comodines,
// paréntesis, etc.), reemplazándolos por espacios.
//
// El guion solo se conserva entre caracteres alfanuméricos para no generar
// el operador de exclusión "-term" de Lucene al inicio de un término.
func sanitizeNameTerm(v string) string {
	var b strings.Builder
	b.Grow(len(v))
	prevAlnum := false
	for _, r := range v {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			prevAlnum = true
		case r == '\'', r == '‘', r == '’', r == 'ʼ':
			b.WriteRune('\'')
			prevAlnum = false
		case r == '-', r == '.', r == '&':
			if r != '-' || prevAlnum {
				b.WriteRune(r)
			}
			prevAlnum = false
		default:
			b.WriteRune(' ')
			prevAlnum = false
		}
	}
	return b.String()
}

// stripNameSymbols reduce el nombre a letras y dígitos. Se usa como segundo
// intento cuando la búsqueda conservando puntuación no arrojó resultados
// (entradas con símbolos que no forman parte del nombre real, p. ej. un
// punto final en "pikachu.").
func stripNameSymbols(v string) string {
	var b strings.Builder
	b.Grow(len(v))
	for _, r := range v {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}
	return b.String()
}

// fieldsWithLetters divide por espacios y descarta tokens sin ninguna letra
// o dígito ("...", "-", "'"), salvo "&", que sí aparece como término en
// nombres indexados como "Pikachu & Zekrom".
func fieldsWithLetters(s string) []string {
	fields := strings.Fields(s)
	tokens := make([]string, 0, len(fields))
	for _, t := range fields {
		if t == "&" || strings.ContainsFunc(t, func(r rune) bool {
			return unicode.IsLetter(r) || unicode.IsDigit(r)
		}) {
			tokens = append(tokens, t)
		}
	}
	return tokens
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
