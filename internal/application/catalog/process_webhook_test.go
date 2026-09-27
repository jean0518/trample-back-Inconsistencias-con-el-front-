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
	cards []catalog.Card
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
	return f.cards, nil
}

// fakePriceRefresher registra a qué cartas se le pidió refrescar el precio y
// simula que solo algunas existen en la base.
type fakePriceRefresher struct {
	vistas     []string
	existentes map[string]bool
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

// El webhook recibe la expansión completa pero solo debe tocar el precio de las
// cartas que ya están dadas de alta. Antes usaba SyncCard (UPSERT) e insertaba
// las ~100 cartas de cada set, llenando `cards` con el catálogo de Scrydex.
func TestExecuteSoloRefrescaPreciosDeCartasExistentes(t *testing.T) {
	scrydex := &fakeWebhookScrydex{cards: []catalog.Card{
		cartaWebhook("dada-de-alta"),
		cartaWebhook("no-dada-de-alta"),
		cartaWebhook("tampoco"),
	}}
	repo := &fakePriceRefresher{existentes: map[string]bool{"dada-de-alta": true}}

	uc := NewProcessWebhookUseCase(NewSearchScrydex(scrydex, fakeTRM{rate: 4000}), repo, nil)
	err := uc.Execute(context.Background(), WebhookEvent{
		ID:   "evt_1",
		Name: "pokemon.expansions.prices.raw_updated",
		Data: WebhookEventData{ExpansionIDs: []string{"base-1"}},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(repo.vistas) != 3 {
		t.Fatalf("se consultaron %d cartas, se esperaban las 3 del set: %v", len(repo.vistas), repo.vistas)
	}
	// Lo importante: se avisó de las tres, pero el repositorio decide cuál
	// existe. El webhook no decide insertar nada.
	for _, id := range []string{"dada-de-alta", "no-dada-de-alta", "tampoco"} {
		found := false
		for _, v := range repo.vistas {
			if v == id {
				found = true
			}
		}
		if !found {
			t.Errorf("la carta %q no se mandó a refrescar", id)
		}
	}
}
