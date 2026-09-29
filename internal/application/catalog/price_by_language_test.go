package catalog

import (
	"errors"
	"math"
	"testing"

	"trample-back/internal/domain/catalog"
	"trample-back/internal/domain/listing"
)

// testTRM es la TRM con la que se construyen las cartas del fixture. Sus valores
// pasan por las mismas reglas que aplica applyTRM en producción, para que el test
// no valide un COP que el código real nunca produciría.
const testTRM = 3309.0

// mw es una impresión de Mew ex tal como la devuelve hoy Scrydex. El caso que
// motivó este código es real: "Mew ex" tiene 32 impresiones y la búsqueda por
// nombre devuelve me55-66 (US$4.93) en lugar de me55-158 (US$90.34), que es la
// que el staff tiene en inventario.
func mw(id, expansion, number, rarity string, marketUSD float64) catalog.Card {
	return catalog.Card{
		ExternalID: id,
		Name:       "Mew ex",
		Number:     number,
		Rarity:     rarity,
		Expansion:  catalog.Expansion{ExternalID: expansion, Name: "30th Celebration"},
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

func TestMatchPrintingEligeLaImpresionCorrecta(t *testing.T) {
	// Devueltas en el orden en que Scrydex las lista para `name:"Mew ex"`.
	candidates := []catalog.Card{
		mw("me55-66", "me55", "66", "Double Rare", 4.93),
		mw("me55-152", "me55", "152", "Special Illustration Rare", 153.14),
		mw("me55-158", "me55", "158", "Futuristic Rare", 90.34),
	}
	ref := identityOf(mw("me55-158", "me55", "158", "Futuristic Rare", 90.34))

	got, ok := matchPrinting(ref, candidates)
	if !ok {
		t.Fatal("matchPrinting no encontró coincidencia")
	}
	if got.ExternalID != "me55-158" {
		t.Fatalf("eligió %s (US$%.2f), se esperaba me55-158", got.ExternalID, got.Variants[0].NMPrice.MarketUSD)
	}
}

func TestMatchPrintingNoAdivinaCuandoNoHayCoincidencia(t *testing.T) {
	// Sin coincidencia debe fallar en vez de devolver el primer resultado: ese
	// es exactamente el bug que escribía el precio de otra carta en el listing.
	candidates := []catalog.Card{
		mw("me55-66", "me55", "66", "Double Rare", 4.93),
		mw("sv4pt5-216", "sv4pt5", "216", "Shiny Ultra Rare", 32.78),
	}
	ref := identityOf(mw("me55-158", "me55", "158", "Futuristic Rare", 90.34))

	if got, ok := matchPrinting(ref, candidates); ok {
		t.Fatalf("matchPrinting devolvió %s, se esperaba que no hubiera coincidencia", got.ExternalID)
	}
}

func TestMatchPrintingEmparejaElEquivalenteJaponés(t *testing.T) {
	// Par real de Scrydex: la misma impresión de Charizard ex en inglés y en
	// japonés. Scrydex le da otro ID y otra expansión (sv3 → sv3_ja), y además
	// cambia la rareza ("Double Rare" → "スーパーレア", RR → SR), así que el
	// emparejamiento tiene que apoyarse en la expansión y el número.
	en := catalog.Card{
		ExternalID: "sv3-125", Name: "Charizard ex", Number: "125",
		Rarity: "Double Rare", RarityCode: "RR",
		Expansion: catalog.Expansion{ExternalID: "sv3", Name: "Scarlet & Violet"},
	}
	ja := catalog.Card{
		ExternalID: "sv3_ja-125", Name: "Charizard ex", Number: "125",
		Rarity: "スーパーレア", RarityCode: "SR",
		Expansion: catalog.Expansion{ExternalID: "sv3_ja", Name: "Scarlet & Violet"},
	}
	// Otro candidato equivocado, con el mismo nombre y otro set.
	other := catalog.Card{
		ExternalID: "sv4a_ja-76", Name: "Charizard ex", Number: "76",
		Rarity: "ダブルレア", Expansion: catalog.Expansion{ExternalID: "sv4a_ja"},
	}

	got, ok := matchPrinting(identityOf(en), []catalog.Card{other, ja})
	if !ok {
		t.Fatal("matchPrinting no encontró el equivalente japonés")
	}
	if got.ExternalID != "sv3_ja-125" {
		t.Fatalf("eligió %s, se esperaba sv3_ja-125", got.ExternalID)
	}
}

func TestBaseExpansionID(t *testing.T) {
	for input, want := range map[string]string{
		"sv3":       "sv3",
		"sv3_ja":    "sv3",
		"m6a_ja":    "m6a",
		"sv4pt5_zh": "sv4pt5",
		"me55":      "me55",
		// Sin sufijo de idioma reconocible se devuelve tal cual.
		"cel25c": "cel25c",
		// El sufijo solo se quita al final: un ID que lo contenga en medio no se
		// toca.
		"ja_set": "ja_set",
	} {
		if got := baseExpansionID(input); got != want {
			t.Fatalf("baseExpansionID(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestMatchPrintingPrefiereElIDExacto(t *testing.T) {
	// Un candidato con el mismo número y expansión pero otro ID no debe ganarle
	// al que coincide exactamente.
	candidates := []catalog.Card{
		mw("otro-158", "me55", "158", "Futuristic Rare", 999),
		mw("me55-158", "me55", "158", "Futuristic Rare", 90.34),
	}
	ref := identityOf(mw("me55-158", "me55", "158", "Futuristic Rare", 90.34))

	got, ok := matchPrinting(ref, candidates)
	if !ok {
		t.Fatal("matchPrinting no encontró coincidencia")
	}
	if got.ExternalID != "me55-158" {
		t.Fatalf("eligió %s, se esperaba me55-158", got.ExternalID)
	}
}

func TestMatchPrintingSinCandidatos(t *testing.T) {
	ref := identityOf(mw("me55-158", "me55", "158", "Futuristic Rare", 90.34))
	if _, ok := matchPrinting(ref, nil); ok {
		t.Fatal("matchPrinting no debe encontrar coincidencia sin candidatos")
	}
}

func TestNormalizeNumber(t *testing.T) {
	for input, want := range map[string]string{
		"158":   "158",
		" 158":  "158",
		"#158":  "158",
		" #158": "158",
	} {
		if got := normalizeNumber(input); got != want {
			t.Fatalf("normalizeNumber(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestApplySpanishPrice(t *testing.T) {
	card := mw("me55-158", "me55", "158", "Futuristic Rare", 90.34)
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
	// cacheada por SearchScrydex y dos consultas en español acumularían el
	// factor (×0.8, ×0.64…).
	if card.Variants[0].NMPrice.MarketUSD != 90.34 {
		t.Fatalf("el original quedó en %v, want 90.34", card.Variants[0].NMPrice.MarketUSD)
	}
	twice := applySpanishPrice(card)
	if twice.Variants[0].NMPrice.MarketUSD != 72.27 {
		t.Fatalf("aplicar el descuento dos veces sobre el original dio %v, want 72.27",
			twice.Variants[0].NMPrice.MarketUSD)
	}
}

// TestApplySpanishPriceRespetaLaReglaDeMercado ata este endpoint con la regla que
// aplica el resto del sistema: repriceListings escribe en inventory_listings el
// COP que sale de SpanishPriceUSD + StandardizedPriceCOP. Si el endpoint devolviera
// otro valor, el catálogo anunciaría un precio que ninguna compra_realiza.
func TestApplySpanishPriceRespetaLaReglaDeMercado(t *testing.T) {
	card := mw("me55-158", "me55", "158", "Futuristic Rare", 90.34)
	got := applySpanishPrice(card).Variants[0].NMPrice

	wantUSD := listing.SpanishPriceUSD(90.34)
	if got.MarketUSD != wantUSD {
		t.Fatalf("MarketUSD = %v, want %v", got.MarketUSD, wantUSD)
	}
	wantCOP := int64(listing.StandardizedPriceCOP(wantUSD, testTRM))
	if got.MarketCOP != wantCOP {
		t.Fatalf("MarketCOP = %v, want %v", got.MarketCOP, wantCOP)
	}
}

// TestApplySpanishPriceSinPrecioBajo comprueba que un "precio bajo" inexistente no
// se inventa: pasarlo por StandardizedPriceCOP devolvería el mínimo fijo de 2.000
// y el catálogo publicaría un precio que Scrydex nunca dio.
func TestApplySpanishPriceSinPrecioBajo(t *testing.T) {
	card := mw("me55-158", "me55", "158", "Futuristic Rare", 90.34)
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

func TestLanguageNameToCode(t *testing.T) {
	for name, want := range map[string]string{
		"Japanese":            "JA",
		"English":             "EN",
		"Korean":              "KO",
		"Chinese Simplified":  "ZH-CN",
		"Chinese Traditional": "ZH-TW",
		// El español no es un idioma indexado: se resuelve sobre el inglés.
		"Spanish": "",
		"Español": "",
		"":        "",
	} {
		if got := LanguageNameToCode(name); got != want {
			t.Fatalf("LanguageNameToCode(%q) = %q, want %q", name, got, want)
		}
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

func TestErroresSonInspeccionables(t *testing.T) {
	// El handler mapea estos errores a 400 para que el front conserve el precio
	// que ya tenía, así que deben seguir siendo identificables con errors.Is.
	wrapped := errors.Join(ErrNotAvailableInLanguage, errors.New("scrydex: HTTP 500"))
	if !errors.Is(wrapped, ErrNotAvailableInLanguage) {
		t.Fatal("ErrNotAvailableInLanguage debe ser inspeccionable")
	}
	if !errors.Is(errors.Join(ErrUnknownLanguage, errors.New("x")), ErrUnknownLanguage) {
		t.Fatal("ErrUnknownLanguage debe ser inspeccionable")
	}
}
