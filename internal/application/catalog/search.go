package catalog

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"sync"
	"time"
	"trample-back/internal/domain/catalog"
	"trample-back/internal/domain/listing"
	"trample-back/internal/ports/out"
)

const searchCacheTTL = 15 * time.Minute

type SearchScrydex struct {
	scrydex out.ScrydexClient
	trm     out.TRMClient

	mu    sync.Mutex
	cache map[string]cachedSearch
}

// cachedSearch guarda los resultados de una búsqueda en memoria para poder
// importar las cartas elegidas sin volver a consultar la API de Scrydex.
type cachedSearch struct {
	GameCode string
	Cards    []catalog.Card
	Expires  time.Time
}

type SearchResult struct {
	SearchID string
	Cards    []catalog.Card
}

func NewSearchScrydex(scrydex out.ScrydexClient, trm out.TRMClient) *SearchScrydex {
	return &SearchScrydex{scrydex: scrydex, trm: trm}
}

func (uc *SearchScrydex) Search(ctx context.Context, p out.SearchParams) (SearchResult, error) {
	if p.GameCode == "" || p.Name == "" {
		return SearchResult{}, fmt.Errorf("game_code y name son requeridos")
	}
	cards, err := uc.scrydex.SearchCards(ctx, p)
	if err != nil {
		return SearchResult{}, err
	}
	cards, err = uc.applyTRM(ctx, cards)
	if err != nil {
		return SearchResult{}, err
	}

	id, err := newSearchID()
	if err != nil {
		return SearchResult{}, err
	}
	uc.store(id, p.GameCode, cards)
	return SearchResult{SearchID: id, Cards: cards}, nil
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

func (uc *SearchScrydex) store(id, gameCode string, cards []catalog.Card) {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	if uc.cache == nil {
		uc.cache = make(map[string]cachedSearch)
	}
	uc.pruneLocked()
	uc.cache[id] = cachedSearch{
		GameCode: gameCode,
		Cards:    cards,
		Expires:  time.Now().Add(searchCacheTTL),
	}
}

func (uc *SearchScrydex) get(id string) (cachedSearch, bool) {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	c, ok := uc.cache[id]
	if !ok {
		return cachedSearch{}, false
	}
	if time.Now().After(c.Expires) {
		delete(uc.cache, id)
		return cachedSearch{}, false
	}
	return c, true
}

func (uc *SearchScrydex) pruneLocked() {
	now := time.Now()
	for id, c := range uc.cache {
		if now.After(c.Expires) {
			delete(uc.cache, id)
		}
	}
}

func newSearchID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generar search_id: %w", err)
	}
	return "search_" + hex.EncodeToString(b), nil
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
			p.MarketCOP = int64(listing.StandardizedPriceCOP(p.MarketUSD, trm))
			p.LowCOP = int64(math.Round(p.LowUSD * trm))
			p.TRMUsed = trm
		}
	}
	return cards, nil
}
