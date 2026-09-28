package catalog

import (
	"context"
	"testing"

	"trample-back/internal/domain/catalog"
	"trample-back/internal/ports/out"
)

// fakeWebhookScrydex devuelve un set completo de cartas, como hace Scrydex en los
// eventos de precio.
type fakeWebhookScrydex struct {
	cards   []catalog.Card
	fetches int
}

func (f *fakeWebhookScrydex) SearchCards(context.Context, out.SearchParams) ([]catalog.Card, error) {
	return nil, nil
}
func (f *fakeWebhookScrydex) FetchCard(context.Context, string, string, []string) (*catalog.Card, error) {
	return nil, nil
}
func (f *fakeWebhookScrydex) FetchExpansions(context.Context, string) ([]catalog.Expansion, error) {
	return nil, nil
}
func (f *fakeWebhookScrydex) FetchExpansionCards(context.Context, string, string) ([]catalog.Card, error) {
	f.fetches++
	return f.cards, nil
}

// fakePriceRefresher registra a qué cartas se le pidió refrescar el precio y
// simula que solo algunas tienen listing en el inventario.
type fakePriceRefresher struct {
	vistas     []string
	existentes map[string]bool
}

func (f *fakePriceRefresher) ListedExternalIDs(context.Context, string, string) (map[string]bool, error) {
	return f.existentes, nil
}

func (f *fakePriceRefresher) RefreshCardPrices(_ context.Context, _ string, card catalog.Card) (bool, error) {
	f.vistas = append(f.vistas, card.ExternalID)
	return f.existentes[card.ExternalID], nil
}

func cartaWebhook(externalID string) catalog.Card {
	return catalog.Card{
		ExternalID: externalID,
		Name:       "Carta " + externalID,
		Variants: []catalog.Variant{{
			Name:    "Normal",
			NMPrice: &catalog.Price{MarketUSD: 1.5},
		}},
	}
}

// El webhook recibe la expansión completa pero solo debe refrescar las cartas
// que tienen listing: las demás ni se mandan al repositorio.
func TestExecuteSoloRefrescaCartasConListing(t *testing.T) {
	scrydex := &fakeWebhookScrydex{cards: []catalog.Card{
		cartaWebhook("listada"),
		cartaWebhook("no-listada"),
		cartaWebhook("tampoco"),
	}}
	repo := &fakePriceRefresher{existentes: map[string]bool{"listada": true}}

	uc := NewProcessWebhookUseCase(NewSearchScrydex(scrydex, fakeTRM{rate: 4000}), repo, nil)
	err := uc.Execute(context.Background(), WebhookEvent{
		ID:   "evt_1",
		Name: "pokemon.expansions.prices.raw_updated",
		Data: WebhookEventData{ExpansionIDs: []string{"base-1"}},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(repo.vistas) != 1 || repo.vistas[0] != "listada" {
		t.Fatalf("se esperaba refrescar solo la carta listada, se refrescaron: %v", repo.vistas)
	}
}

// Una expansión sin nada en el inventario no debe gastar la llamada a Scrydex.
func TestExecuteSaltaExpansionesSinListing(t *testing.T) {
	scrydex := &fakeWebhookScrydex{cards: []catalog.Card{cartaWebhook("x")}}
	repo := &fakePriceRefresher{existentes: map[string]bool{}}

	uc := NewProcessWebhookUseCase(NewSearchScrydex(scrydex, fakeTRM{rate: 4000}), repo, nil)
	err := uc.Execute(context.Background(), WebhookEvent{
		ID:   "evt_2",
		Name: "pokemon.expansions.prices.raw_updated",
		Data: WebhookEventData{ExpansionIDs: []string{"base-1", "base-2"}},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if scrydex.fetches != 0 {
		t.Fatalf("se consultó Scrydex %d veces para expansiones sin listing", scrydex.fetches)
	}
	if len(repo.vistas) != 0 {
		t.Fatalf("no se esperaba refrescar ninguna carta: %v", repo.vistas)
	}
}
