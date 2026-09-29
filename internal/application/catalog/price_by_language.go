package catalog

import (
	"context"
	"errors"
	"fmt"
	"math"

	"trample-back/internal/domain/catalog"
	"trample-back/internal/domain/listing"
)

// ErrNotAvailableInLanguage indica que no se pudo leer la impresión de Scrydex,
// así que no hay precio para ese listing. El front lo traduce a "no disponible"
// y conserva el precio que ya tenía, que es preferible a mostrar el de otra
// carta.
var ErrNotAvailableInLanguage = errors.New("carta no disponible en ese idioma")

// PriceByLanguageInput pide el precio de una impresión concreta en un idioma.
type PriceByLanguageInput struct {
	GameCode string
	// ExternalID es el ID de Scrydex de la impresión que el staff tiene en
	// inventario. Es obligatorio: es lo único que identifica la impresión, y los
	// nombres no son únicos ("Mew ex" tiene 32 impresiones con precios de US$1 a
	// US$15.000).
	ExternalID string
	// Name solo se usa para identificar la carta en los errores y en el log. No
	// participa en la búsqueda: la impresión se pide por ID.
	Name string
	// Language es la etiqueta del listing. Salvo para el español de Pokémon, no
	// cambia el precio.
	Language string
	Variants []string
}

// PriceByLanguageUseCase devuelve el precio con el que se publica una impresión
// en un idioma concreto.
//
// No consulta el precio de ese idioma en Scrydex. Scrydex no indexa el español y,
// para el resto de los idiomas, la impresión que el staff tiene en inventario se
// publica al precio de mercado de esa misma impresión — el que el webhook de
// precios mantiene al día—, sin importar qué idioma esté escrita la etiqueta del
// listing. Buscar por idioma solo producía dos fallos: un precio de otra
// impresión cuando los nombres no eran únicos, y un "no disponible en ese idioma"
// cuando la impresión no existía traducida, que obligaba al staff a dejar el
// precio a mano.
//
// La única excepción es el español de Pokémon, que se publica al 80 % del
// precio de mercado porque Scrydex no lo indexa y no hay precio de referencia.
// Ver listing.IsDiscountedLanguage, que es la fuente de esa regla.
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

	card, err := uc.search.FetchOne(ctx, in.GameCode, in.ExternalID, in.Variants)
	if err != nil {
		name := in.Name
		if name == "" {
			name = in.ExternalID
		}
		// El sentinel va con %w para que el handler lo mapee a 400, y la causa de
		// Scrydex también, para que los logs conserven el error real.
		return catalog.Card{}, fmt.Errorf("%w: no se pudo leer la impresión %s de Scrydex: %w",
			ErrNotAvailableInLanguage, name, err)
	}

	if listing.IsDiscountedLanguage(in.GameCode, in.Language) {
		return applySpanishPrice(*card), nil
	}
	return *card, nil
}

// applySpanishPrice devuelve una copia de la carta con sus precios de mercado
// convertidos al del español. Las variantes sin precio se saltan en lugar de
// quedar en 0, que es lo que ocurría al asignar sobre un puntero nil.
//
// El descuento no se aplica multiplicando el COP que ya vino redondeado: se
// recalcula desde el USD descontado con la misma regla de siempre
// (SpanishPriceUSD + StandardizedPriceCOP), igual que hace repriceListings al
// re-preciar un listing en español. Si no, el catálogo anunciaría un COP que
// nunca se escribiría en inventory_listings — 299.000 × 0,80 da 239.200, pero la
// regla sobre 72,27 USD da 240.000.
func applySpanishPrice(card catalog.Card) catalog.Card {
	if len(card.Variants) == 0 {
		return card
	}

	// Se copia el slice de variantes antes de escribir. catalog.Card se pasa por
	// valor, pero el slice comparte el arreglo subyacente, así que asignar sobre
	// card.Variants[i] modificaría también la carta de quien la pasó. El precio
	// además es un puntero compartido, así que también se copia por valor:
	// mutarlo en el sitio escribiría sobre la carta cacheada.
	variants := make([]catalog.Variant, len(card.Variants))
	copy(variants, card.Variants)
	card.Variants = variants

	for i := range card.Variants {
		if card.Variants[i].NMPrice == nil {
			continue
		}
		p := *card.Variants[i].NMPrice

		// Regla de precio de mercado: ver applyTRM en search.go. Se conserva
		// para que MarketCOP siga siendo derivable de MarketUSD.
		p.MarketUSD = listing.SpanishPriceUSD(p.MarketUSD)
		p.MarketCOP = int64(listing.StandardizedPriceCOP(p.MarketUSD, p.TRMUsed))

		// El precio bajo es opcional: Scrydex no siempre lo trae. Con LowUSD en 0
		// se deja en 0 en vez de pasarlo por StandardizedPriceCOP, que devolvería
		// el mínimo fijo de 2.000 y publicaría un "precio bajo" que no existe.
		if p.LowUSD > 0 {
			p.LowUSD = listing.SpanishPriceUSD(p.LowUSD)
			p.LowCOP = int64(math.Round(p.LowUSD * p.TRMUsed))
		} else {
			p.LowUSD, p.LowCOP = 0, 0
		}

		card.Variants[i].NMPrice = &p
	}
	return card
}
