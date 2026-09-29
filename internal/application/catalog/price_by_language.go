package catalog

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"trample-back/internal/domain/catalog"
	"trample-back/internal/domain/listing"
	"trample-back/internal/ports/out"
)

// ErrNotAvailableInLanguage indica que Scrydex no tiene esa impresión en el
// idioma pedido, o que no se pudo confirmar con certeza qué impresión es. El
// front lo traduce a "no disponible en ese idioma" y conserva el precio que ya
// tenía, que es preferible a mostrar el de otra carta.
var ErrNotAvailableInLanguage = errors.New("carta no disponible en ese idioma")

// ErrUnknownLanguage indica que el nombre de idioma no tiene código en Scrydex.
var ErrUnknownLanguage = errors.New("idioma no reconocido")

// PriceByLanguageInput pide el precio de una impresión concreta en un idioma.
type PriceByLanguageInput struct {
	GameCode string
	// ExternalID es el ID de Scrydex de la impresión que el staff tiene en
	// inventario. Es obligatorio: sin él no hay forma de saber cuál de las
	// impresiones con el mismo nombre es la correcta.
	ExternalID    string
	Name          string
	ExpansionCode string
	Rarity        string
	Language      string
	Variants      []string
}

// PriceByLanguageUseCase devuelve el precio de mercado de una carta en un idioma
// concreto.
//
// El problema que resuelve: los nombres de carta no son únicos. "Mew ex" tiene
// 32 impresiones en Scrydex con precios que van de US$1 a US$15.000, y la
// búsqueda por nombre devuelve la que Scrydex ordene primero, no la que el
// staff tiene registrada. Tomar el primer resultado produce precios de otra
// carta — y como el panel de edición los escribe en el listing, se perdía el
// precio real. Por eso la impresión se resuelve por identidad y nunca por
// posición en la lista.
type PriceByLanguageUseCase struct {
	search *SearchScrydex
}

func NewPriceByLanguageUseCase(search *SearchScrydex) *PriceByLanguageUseCase {
	return &PriceByLanguageUseCase{search: search}
}

func (uc *PriceByLanguageUseCase) Execute(ctx context.Context, in PriceByLanguageInput) (catalog.Card, error) {
	if in.GameCode == "" {
		return catalog.Card{}, fmt.Errorf("game_code es requerido")
	}
	if in.ExternalID == "" {
		// Sin ID no se puede garantizar que el precio sea de esta impresión y
		// un precio equivocado es peor que ninguna respuesta.
		return catalog.Card{}, fmt.Errorf("external_id es requerido para consultar el precio por idioma")
	}
	ref, err := uc.resolvePrinting(ctx, in)
	if err != nil {
		return catalog.Card{}, err
	}

	// El español no existe en el índice de Scrydex, así que su precio se deriva
	// de la impresión en inglés. Como el español no cambia el ID de Scrydex
	// (no hay una impresión en español que buscar), la carta de referencia ya
	// *es* la respuesta: no hace falta una segunda consulta.
	if listing.IsSpanishLanguage(in.Language) {
		return applyLanguageFactor(ref, listing.SpanishPriceFactor), nil
	}

	code := LanguageNameToCode(in.Language)
	if code == "" {
		return catalog.Card{}, fmt.Errorf("%w: %s", ErrUnknownLanguage, in.Language)
	}
	return uc.indexedPrice(ctx, in, ref, code)
}

// resolvePrinting descarga la impresión exacta desde Scrydex para tener su
// número impreso, su rareza y su expansión: los datos que permiten reconocer la
// misma carta dentro del índice de otro idioma.
func (uc *PriceByLanguageUseCase) resolvePrinting(ctx context.Context, in PriceByLanguageInput) (catalog.Card, error) {
	card, err := uc.search.FetchOne(ctx, in.GameCode, in.ExternalID, in.Variants)
	if err != nil {
		return catalog.Card{}, fmt.Errorf("%w: no se pudo leer la impresión %s de Scrydex: %v",
			ErrNotAvailableInLanguage, in.ExternalID, err)
	}
	return *card, nil
}

// indexedPrice consulta el precio en el idioma que Scrydex sí indexa (japonés,
// coreano, francés…) y elige la impresión que corresponde.
func (uc *PriceByLanguageUseCase) indexedPrice(ctx context.Context, in PriceByLanguageInput, ref catalog.Card, languageCode string) (catalog.Card, error) {
	// No se filtra por expansión: las impresiones en otro idioma tienen su
	// propia expansión en Scrydex (sv4pt5_ja en vez de sv4pt5), así que el
	// filtro de la versión inglesa las excluiría a todas. La impresión se
	// selecciona después, comparando número impreso y rareza.
	result, err := uc.search.Search(ctx, out.SearchParams{
		GameCode:      in.GameCode,
		Name:          in.Name,
		ExpansionCode: in.ExpansionCode,
		Rarity:        in.Rarity,
		Variants:      in.Variants,
		LanguageCode:  languageCode,
	})
	if err != nil {
		return catalog.Card{}, fmt.Errorf("%w: %v", ErrNotAvailableInLanguage, err)
	}

	card, ok := matchPrinting(identityOf(ref), result.Cards)
	if !ok {
		return catalog.Card{}, fmt.Errorf("%w: %s (%s) no tiene una impresión equivalente",
			ErrNotAvailableInLanguage, in.Name, in.Language)
	}
	return card, nil
}

// printingIdentity son los datos que distinguen una impresión concreta dentro de
// un índice de idioma. Se separa de catalog.Card para poder probar el emparejador
// sin tocar la red.
//
// Deliberadamente NO incluye la rareza. Scrydex la devuelve traducida al idioma
// del índice —"Double Rare" en inglés, "ダブルレア" en japonés— e incluso el
// rarity_code puede diferir para la misma impresión (sv3-125 es RR en inglés y SR
// en japonés). El número de colección dentro de la expansión sí es estable entre
// idiomas, y con la expansión es lo único que permite reconocer la misma carta
// en otro índice.
type printingIdentity struct {
	ExternalID string
	Number     string
	// ExpansionKey es el ID de la expansión sin el sufijo de idioma, que Scrydex
	// agrega para las impresiones no inglesas (sv3 → sv3_ja, m6a → m6a_ja).
	ExpansionKey string
}

func identityOf(c catalog.Card) printingIdentity {
	return printingIdentity{
		ExternalID:   c.ExternalID,
		Number:       normalizeNumber(c.Number),
		ExpansionKey: baseExpansionID(c.Expansion.ExternalID),
	}
}

// baseExpansionID quita el sufijo de idioma del ID de expansión para poder
// comparar la misma expansión entre índices. Scrydex usa el sufijo _ja, _zh, _ko,
// _en, _fr, _de o _it.
func baseExpansionID(expansionID string) string {
	for _, suffix := range []string{"_ja", "_zh", "_ko", "_en", "_fr", "_de", "_it"} {
		if trimmed, ok := strings.CutSuffix(expansionID, suffix); ok {
			return trimmed
		}
	}
	return expansionID
}

// matchPrinting elige, entre los candidatos de una búsqueda por nombre, la
// impresión que corresponde a la referencia. Se prueban dos criterios, del más
// al menos estricto:
//
//  1. Mismo ID de Scrydex — la impresión es literalmente la misma, o el índice
//     de destino es el mismo que el de origen.
//  2. Misma expansión y mismo número de colección.
//
// El segundo es el que empareja una impresión inglesa con su equivalente
// japonesa, que Scrydex indexa con otro ID y otra expansión (sv3-125 →
// sv3_ja-125). No hay un tercer criterio: cuando la colección no tiene
// equivalente en el idioma pedido, inventarse uno devolvería el precio de otra
// carta, que es exactamente el bug que este código evita.
func matchPrinting(ref printingIdentity, candidates []catalog.Card) (catalog.Card, bool) {
	const (
		noMatch   = 0
		byPrintng = 1
		byScrydex = 2
	)

	best, bestRank := -1, noMatch
	for i, c := range candidates {
		id := identityOf(c)

		rank := noMatch
		switch {
		case ref.ExternalID != "" && id.ExternalID == ref.ExternalID:
			rank = byScrydex
		case ref.ExpansionKey != "" && ref.Number != "" &&
			id.ExpansionKey == ref.ExpansionKey && id.Number == ref.Number:
			rank = byPrintng
		}

		if rank > bestRank {
			best, bestRank = i, rank
		}
	}
	if best < 0 {
		return catalog.Card{}, false
	}
	return candidates[best], true
}

// normalizeNumber normaliza el número de colección para comparar "158", " 158"
// y "#158" como la misma cosa. Scrydex lo devuelve sin "#", pero el front lo
// muestra con "#" y en algunos juegos el collector number no coincide con el
// impreso.
func normalizeNumber(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "#")
}

// applyLanguageFactor devuelve una copia de la carta con todos los precios de
// sus variantes multiplicados por factor. Las variantes sin precio se saltan en
// lugar de quedar en 0, que es lo que ocurría al asignar sobre un puntero nil.
func applyLanguageFactor(card catalog.Card, factor float64) catalog.Card {
	if factor == 1 || len(card.Variants) == 0 {
		return card
	}

	// Se copia el slice de variantes antes de escribir. catalog.Card se pasa por
	// valor, pero el slice comparte el arreglo subyacente, así que asignar sobre
	// card.Variants[i] modificaría también la carta de quien la pasó — y el
	// resultado de la búsqueda queda cacheado, con lo que dos consultas en
	// español sobre la misma carta acumularían el factor (×0.8, ×0.64, ×0.51…).
	variants := make([]catalog.Variant, len(card.Variants))
	copy(variants, card.Variants)
	card.Variants = variants

	for i := range card.Variants {
		if card.Variants[i].NMPrice == nil {
			continue
		}
		p := *card.Variants[i].NMPrice
		p.MarketUSD = math.Round(p.MarketUSD*factor*100) / 100
		p.LowUSD = math.Round(p.LowUSD*factor*100) / 100
		p.MarketCOP = int64(math.Round(float64(p.MarketCOP) * factor))
		p.LowCOP = int64(math.Round(float64(p.LowCOP) * factor))
		card.Variants[i].NMPrice = &p
	}
	return card
}

// LanguageNameToCode convierte el nombre completo de un idioma al código que usa
// Scrydex en su campo language_code (p. ej. "Japanese" → "JA"). Devuelve "" si el
// idioma no está mapeado.
//
// Acepta las etiquetas en español además de las de Scrydex porque la misma
// lista de idiomas se usa en los dos flujos: el panel guarda "Japonés" en
// inventory_listings.language y envía "Japanese" a este endpoint. Aceptar las
// dos evita que el backend dependa de que el front aplique la conversión.
//
// El español no aparece en el mapa a propósito: no es un idioma indexado sino un
// caso especial que se resuelve sobre el precio inglés.
func LanguageNameToCode(name string) string {
	codes := map[string]string{
		"Japanese":            "JA",
		"English":             "EN",
		"French":              "FR",
		"German":              "DE",
		"Italian":             "IT",
		"Korean":              "KO",
		"Chinese Traditional": "ZH-TW",
		"Chinese Simplified":  "ZH-CN",
		"Portuguese":          "PT",
		"Polish":              "PL",
		"Russian":             "RU",
		"Dutch":               "NL",

		// Mismos idiomas con la etiqueta que usa el panel en español.
		"Japonés":            "JA",
		"Inglés":             "EN",
		"Francés":            "FR",
		"Alemán":             "DE",
		"Italiano":           "IT",
		"Coreano":            "KO",
		"Chino tradicional":  "ZH-TW",
		"Chino simplificado": "ZH-CN",
		"Portugués":          "PT",
		"Polaco":             "PL",
		"Ruso":               "RU",
		"Neerlandés":         "NL",
	}
	return codes[name]
}
