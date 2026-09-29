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
	cards      []catalog.Card
	fetches    int
	expansions []catalog.Expansion
	// listas cuenta las veces que se pidió la lista de expansiones.
	listas int
}

func (f *fakeWebhookScrydex) SearchCards(context.Context, out.SearchParams) ([]catalog.Card, error) {
	return nil, nil
}
func (f *fakeWebhookScrydex) FetchCard(context.Context, string, string, []string) (*catalog.Card, error) {
	return nil, nil
}
func (f *fakeWebhookScrydex) FetchExpansions(context.Context, string) ([]catalog.Expansion, error) {
	f.listas++
	return f.expansions, nil
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

// Las expansiones que existen en la base para los tests; el resto se reporta
// como "no está en la base".
func (f *fakePriceRefresher) FindExpansionName(_ context.Context, _ string, id string) (string, bool, error) {
	if id == "base-2" {
		return "Base Set 2", true, nil
	}
	return "", false, nil
}

func (f *fakePriceRefresher) RefreshCardPrices(_ context.Context, _ string, card catalog.Card) ([]catalog.PriceUpdate, error) {
	f.vistas = append(f.vistas, card.ExternalID)
	if !f.existentes[card.ExternalID] {
		return nil, nil
	}
	return []catalog.PriceUpdate{{VariantName: "Normal", HadPrice: true, OldUSD: 1, NewUSD: 1.5}}, nil
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

	uc := NewProcessWebhookUseCase(NewSearchScrydex(scrydex, fakeTRM{rate: 4000}), repo, nil, nil)
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

	uc := NewProcessWebhookUseCase(NewSearchScrydex(scrydex, fakeTRM{rate: 4000}), repo, nil, nil)
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

type fakeNotifier struct {
	llamadas int
	cambios  []catalog.CardPriceChange
	saltadas []catalog.SkippedExpansion
}

func (f *fakeNotifier) NotifyPriceReport(_ context.Context, report catalog.PriceReport) error {
	f.llamadas++
	f.cambios = append(f.cambios, report.Changes...)
	f.saltadas = append(f.saltadas, report.Skipped...)
	return nil
}

// fakeSamePriceRefresher simula una carta listada cuyo precio no cambió.
type fakeSamePriceRefresher struct{ fakePriceRefresher }

func (f *fakeSamePriceRefresher) RefreshCardPrices(context.Context, string, catalog.Card) ([]catalog.PriceUpdate, error) {
	return []catalog.PriceUpdate{{VariantName: "Normal", HadPrice: true, OldUSD: 1.5, NewUSD: 1.5}}, nil
}

// Solo se avisa por Telegram de las cartas listadas cuyo precio cambió.
func TestExecuteNotificaSoloCambiosReales(t *testing.T) {
	event := WebhookEvent{
		ID:   "evt_3",
		Name: "pokemon.expansions.prices.raw_updated",
		Data: WebhookEventData{ExpansionIDs: []string{"base-1"}},
	}
	scrydex := &fakeWebhookScrydex{cards: []catalog.Card{cartaWebhook("listada"), cartaWebhook("no-listada")}}

	notifier := &fakeNotifier{}
	repo := &fakePriceRefresher{existentes: map[string]bool{"listada": true}}
	uc := NewProcessWebhookUseCase(NewSearchScrydex(scrydex, fakeTRM{rate: 4000}), repo, notifier, nil)
	if err := uc.Execute(context.Background(), event); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if notifier.llamadas != 1 || len(notifier.cambios) != 1 || notifier.cambios[0].ExternalID != "listada" {
		t.Fatalf("se esperaba un aviso con la carta listada, hubo %d avisos: %+v", notifier.llamadas, notifier.cambios)
	}

	notifier = &fakeNotifier{}
	same := &fakeSamePriceRefresher{fakePriceRefresher{existentes: map[string]bool{"listada": true}}}
	uc = NewProcessWebhookUseCase(NewSearchScrydex(scrydex, fakeTRM{rate: 4000}), same, notifier, nil)
	if err := uc.Execute(context.Background(), event); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if notifier.llamadas != 0 {
		t.Fatalf("no se esperaba aviso si el precio no cambió, hubo %d", notifier.llamadas)
	}
}

// Las expansiones sin listings se reportan con el motivo: no existe en la base
// o existe pero no tiene stock.
func TestExecuteReportaExpansionesSaltadasConMotivo(t *testing.T) {
	scrydex := &fakeWebhookScrydex{expansions: []catalog.Expansion{{ExternalID: "base-1", Name: "Base Set"}}}
	repo := &fakePriceRefresher{existentes: map[string]bool{}}
	notifier := &fakeNotifier{}

	uc := NewProcessWebhookUseCase(NewSearchScrydex(scrydex, fakeTRM{rate: 4000}), repo, notifier, nil)
	event := WebhookEvent{
		ID:   "evt_4",
		Name: "pokemon.expansions.prices.raw_updated",
		Data: WebhookEventData{ExpansionIDs: []string{"base-1", "base-2"}},
	}
	if err := uc.Execute(context.Background(), event); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if notifier.llamadas != 1 || len(notifier.saltadas) != 2 {
		t.Fatalf("se esperaba un aviso con 2 expansiones saltadas: %d avisos, %+v", notifier.llamadas, notifier.saltadas)
	}
	want := []catalog.SkippedExpansion{
		// No está en la base: el nombre sale de la lista de Scrydex.
		{ID: "base-1", Name: "Base Set", Reason: catalog.SkipNotInDB},
		{ID: "base-2", Name: "Base Set 2", Reason: catalog.SkipNoStock},
	}
	for i, w := range want {
		if notifier.saltadas[i] != w {
			t.Errorf("saltada %d = %+v, want %+v", i, notifier.saltadas[i], w)
		}
	}

	// La lista de nombres se guarda en memoria: un segundo evento no vuelve a
	// pedirla a Scrydex.
	if err := uc.Execute(context.Background(), event); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if scrydex.listas != 1 {
		t.Fatalf("la lista de expansiones se pidió %d veces, se esperaba 1", scrydex.listas)
	}
}
