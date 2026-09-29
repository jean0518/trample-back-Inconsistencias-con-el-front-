package catalog

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"

	"trample-back/internal/domain/catalog"
	"trample-back/internal/domain/listing"
	"trample-back/internal/ports/out"
)

// testTRM es la TRM con la que se construyen las cartas del fixture. Sus valores
// pasan por las mismas reglas que aplica applyTRM en producción, para que el test
// no valide un COP que el código real nunca produciría.
const testTRM = 3309.0

// fakeFetchScrydex devuelve una única impresión, como hace Scrydex al pedir una
// carta por ID.
type fakeFetchScrydex struct {
	out.ScrydexClient
	card *catalog.Card
	err  error
	// asks cuenta las consultas y guarda el ID pedido, para comprobar que la
	// impresión se resuelve por identidad y no por nombre.
	asks    int
	askedID string
}

func (f *fakeFetchScrydex) FetchCard(_ context.Context, _, externalID string, _ []string) (*catalog.Card, error) {
	f.asks++
	f.askedID = externalID
	if f.err != nil {
		return nil, f.err
	}
	return f.card, nil
}

func (f *fakeFetchScrydex) FetchExpansions(context.Context, string) ([]catalog.Expansion, error) {
	return nil, nil
}

// mewCard es una impresión de Mew ex tal como la devuelve hoy Scrydex: 90,34 USD
// de precio de mercado.
func mewCard() catalog.Card {
	const marketUSD = 90.34
	return catalog.Card{
		ExternalID: "me55-158",
		Name:       "Mew ex",
		Number:     "158",
		Rarity:     "Futuristic Rare",
		Expansion:  catalog.Expansion{ExternalID: "me55", Name: "30th Celebration"},
		Variants: []catalog.Variant{{
			Name: "holofoil",
			NMPrice: &catalog.Price{
				MarketUSD: marketUSD,
				MarketCOP: int64(listing.StandardizedPriceCOP(marketUSD, testTRM)),
				LowUSD:    math.Round(marketUSD*0.6*100) / 100,
				LowCOP:    int64(math.Round(math.Round(marketUSD*0.6*100) / 100 * testTRM)),
				TRMUsed:   testTRM,
			},
		}},
	}
}

func newPriceByLanguage(card catalog.Card) (*PriceByLanguageUseCase, *fakeFetchScrydex) {
	scrydex := &fakeFetchScrydex{card: &card}
	return NewPriceByLanguageUseCase(NewSearchScrydex(scrydex, fakeTRM{rate: testTRM})), scrydex
}

// TestExecuteSoloDescuentaElEspanolDePokemon recorre la regla completa: el único
// idioma con precio propio es el español de Pokémon. Cualquier otro idioma, en
// cualquier juego, devuelve el precio de mercado tal cual.
func TestExecuteSoloDescuentaElEspanolDePokemon(t *testing.T) {
	cases := []struct {
		game     string
		language string
		discount bool
	}{
		{"pokemon", "Español", true},
		{"pokemon", "Spanish", true},
		// El resto de los idiomas de Pokémon va al precio de mercado.
		{"pokemon", "Japonés", false},
		{"pokemon", "Inglés", false},
		{"pokemon", "Coreano", false},
		// Y el español de otros juegos también: el descuento es de Pokémon.
		{"mtg", "Español", false},
		{"mtg", "Spanish", false},
		{"mtg", "French", false},
		{"riftbound", "Español", false},
		// Sin idioma tampoco hay descuento.
		{"pokemon", "", false},
	}

	for _, c := range cases {
		uc, _ := newPriceByLanguage(mewCard())
		got, err := uc.Execute(t.Context(), PriceByLanguageInput{
			GameCode:   c.game,
			ExternalID: "me55-158",
			Name:       "Mew ex",
			Language:   c.language,
		})
		if err != nil {
			t.Fatalf("%s/%s: %v", c.game, c.language, err)
		}
		p := got.Variants[0].NMPrice
		want := 90.34
		if c.discount {
			want = listing.SpanishPriceUSD(90.34)
		}
		if p.MarketUSD != want {
			t.Errorf("%s/%s: MarketUSD = %v, want %v", c.game, c.language, p.MarketUSD, want)
		}
		// El COP siempre tiene que ser derivable del USD: si el precio de venta
		// se anunciara con un COP que no sale de la regla, el catálogo y el
		// re-preciado mostrarían precios distintos para la misma carta.
		if wantCOP := int64(listing.StandardizedPriceCOP(p.MarketUSD, testTRM)); p.MarketCOP != wantCOP {
			t.Errorf("%s/%s: MarketCOP = %v, want %v", c.game, c.language, p.MarketCOP, wantCOP)
		}
	}
}

// TestExecuteResuelveLaImpresionPorID comprueba que el precio siempre viene de la
// impresión pedida y nunca del primer resultado de una búsqueda por nombre: "Mew
// ex" tiene 32 impresiones, de US$1 a US$15.000.
func TestExecuteResuelveLaImpresionPorID(t *testing.T) {
	uc, scrydex := newPriceByLanguage(mewCard())

	if _, err := uc.Execute(t.Context(), PriceByLanguageInput{
		GameCode:   "pokemon",
		ExternalID: "me55-158",
		Name:       "Mew ex",
		Language:   "Japonés",
	}); err != nil {
		t.Fatal(err)
	}
	if scrydex.asks != 1 {
		t.Fatalf("consultó Scrydex %d veces, want 1", scrydex.asks)
	}
	if scrydex.askedID != "me55-158" {
		t.Fatalf("pidió la impresión %q, want me55-158", scrydex.askedID)
	}
}

// TestExecuteNoFallaPorIdiomaSinTraduccion es el caso que reportaba el panel: una
// impresión japonesa no existente en el índice japonés igual devuelve precio, en
// vez del "no disponible en ese idioma" que obligaba a dejarlo a mano.
func TestExecuteNoFallaPorIdiomaSinTraduccion(t *testing.T) {
	uc, _ := newPriceByLanguage(mewCard())

	got, err := uc.Execute(t.Context(), PriceByLanguageInput{
		GameCode:   "pokemon",
		ExternalID: "me55-158",
		Language:   "Japonés",
	})
	if err != nil {
		t.Fatalf("un idioma sin precio propio no debe dar error: %v", err)
	}
	if got.Variants[0].NMPrice.MarketUSD != 90.34 {
		t.Fatalf("MarketUSD = %v, want 90.34", got.Variants[0].NMPrice.MarketUSD)
	}
}

func TestExecuteSinExternalIDNoConsulta(t *testing.T) {
	// Sin external_id no hay forma de saber qué impresión se pidió, así que no
	// se debe llamar a Scrydex ni devolver el primer resultado por nombre.
	uc := NewPriceByLanguageUseCase(nil)

	_, err := uc.Execute(t.Context(), PriceByLanguageInput{
		GameCode: "pokemon",
		Name:     "Mew ex",
		Language: "Español",
	})
	if err == nil {
		t.Fatal("se esperaba error sin external_id")
	}
	if errors.Is(err, ErrNotAvailableInLanguage) {
		t.Fatalf("el error debería ser de validación, no de disponibilidad: %v", err)
	}
}

func TestExecutePropagaElFalloDeScrydex(t *testing.T) {
	scrydex := &fakeFetchScrydex{err: fmt.Errorf("HTTP 500")}
	uc := NewPriceByLanguageUseCase(NewSearchScrydex(scrydex, fakeTRM{rate: testTRM}))

	_, err := uc.Execute(t.Context(), PriceByLanguageInput{
		GameCode:   "pokemon",
		ExternalID: "me55-158",
		Name:       "Mew ex",
		Language:   "Español",
	})
	if !errors.Is(err, ErrNotAvailableInLanguage) {
		t.Fatalf("el error debe ser inspeccionable para mapearlo a 400: %v", err)
	}
	if !errors.Is(err, scrydex.err) {
		t.Fatalf("debe conservar la causa de Scrydex: %v", err)
	}
}

// TestApplySpanishPrice verifica el cálculo del descuento y que no se filtre a la
// carta cacheada.
func TestApplySpanishPrice(t *testing.T) {
	card := mewCard()
	card.Variants = append(card.Variants, catalog.Variant{Name: "normal", NMPrice: nil})

	got := applySpanishPrice(card)

	holo := got.Variants[0].NMPrice
	// 90.34 * 0.80 = 72.272 -> 72.27.
	if holo.MarketUSD != 72.27 {
		t.Fatalf("MarketUSD = %v, want 72.27", holo.MarketUSD)
	}
	// El COP no es 299.000 * 0,80 = 239.200: se recalcula desde el USD
	// descontado con la regla de precio, que es la que escribe
	// inventory_listings. 72.27 * 3309 = 239.141 -> 240.000 al millar.
	if holo.MarketCOP != 240000 {
		t.Fatalf("MarketCOP = %v, want 240000", holo.MarketCOP)
	}
	// El precio bajo sigue la misma regla que applyTRM: USD * TRM sin redondear
	// al millar. 54.20 -> 43.36 USD, 43.36 * 3309 = 143.478.
	if holo.LowUSD != 43.36 {
		t.Fatalf("LowUSD = %v, want 43.36", holo.LowUSD)
	}
	if holo.LowCOP != 143478 {
		t.Fatalf("LowCOP = %v, want 143478", holo.LowCOP)
	}
	if got.Variants[1].NMPrice != nil {
		t.Fatal("una variante sin precio debe quedar en nil, no en 0")
	}
	// La copia debe ser real: si compartiera el arreglo de variantes o el
	// puntero del precio, el descuento se escribiría también sobre la carta
	// cacheada por SearchScrydex.
	if card.Variants[0].NMPrice.MarketUSD != 90.34 {
		t.Fatalf("el original quedó en %v, want 90.34", card.Variants[0].NMPrice.MarketUSD)
	}
	twice := applySpanishPrice(card)
	if twice.Variants[0].NMPrice.MarketUSD != 72.27 {
		t.Fatalf("aplicar el descuento dos veces sobre el original dio %v, want 72.27",
			twice.Variants[0].NMPrice.MarketUSD)
	}
}

// TestApplySpanishPriceSinPrecioBajo comprueba que un "precio bajo" inexistente no
// se inventa: pasarlo por StandardizedPriceCOP devolvería el mínimo fijo de 2.000
// y el catálogo publicaría un precio que Scrydex nunca dio.
func TestApplySpanishPriceSinPrecioBajo(t *testing.T) {
	card := mewCard()
	card.Variants[0].NMPrice.LowUSD = 0
	card.Variants[0].NMPrice.LowCOP = 0

	got := applySpanishPrice(card).Variants[0].NMPrice

	if got.LowUSD != 0 || got.LowCOP != 0 {
		t.Fatalf("precio bajo = %v/%v, want 0/0", got.LowUSD, got.LowCOP)
	}
	if got.MarketCOP == int64(listing.FixedPriceUnderUSDCOP) {
		t.Fatal("el precio bajo ausente no debe caer al mínimo fijo de 2.000")
	}
}

func TestIsSpanishLanguage(t *testing.T) {
	// Acepta las dos etiquetas: la que persiste el backend en el listing y la
	// que envía el front al endpoint de precio.
	for _, name := range []string{"Spanish", "Español"} {
		if !listing.IsSpanishLanguage(name) {
			t.Fatalf("IsSpanishLanguage(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"", "Inglés", "English", "Japonés", "Japanese"} {
		if listing.IsSpanishLanguage(name) {
			t.Fatalf("IsSpanishLanguage(%q) = true, want false", name)
		}
	}
}

func TestErroresSonInspeccionables(t *testing.T) {
	// El handler mapea este error a 400 para que el front conserve el precio que
	// ya tenía, así que debe seguir siendo identificable con errors.Is.
	wrapped := errors.Join(ErrNotAvailableInLanguage, errors.New("scrydex: HTTP 500"))
	if !errors.Is(wrapped, ErrNotAvailableInLanguage) {
		t.Fatal("ErrNotAvailableInLanguage debe ser inspeccionable")
	}
}
