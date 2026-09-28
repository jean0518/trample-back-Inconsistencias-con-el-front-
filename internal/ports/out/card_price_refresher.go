package out

import (
	"context"

	"trample-back/internal/domain/catalog"
)

// CardPriceRefresher actualiza el precio de mercado de cartas que YA existen en
// la base, sin crear nada nuevo.
//
// Es un puerto aparte de CardRepository a propósito. El webhook de Scrydex antes
// usaba SyncCard, que hace UPSERT: como los eventos de precio traen la expansión
// completa, acababa insertando las ~100 cartas de cada set aunque nadie las
// hubiera dado de alta en un listing. Eso llenaba `cards` con el catálogo entero
// de Scrydex (15.689 cartas de Pokémon en dos días) y el panel mostraba un
// "catálogo" que no existía en el inventario real.
//
// Con este puerto el webhook solo puede tocar lo que ya está dado de alta: una
// carta que no existe en `cards` se ignora, y el alta sigue siendo
// responsabilidad del flujo de listings.
type CardPriceRefresher interface {
	// ListedExternalIDs devuelve los external_id de las cartas de una expansión
	// que tienen al menos un listing en el inventario. El webhook lo consulta
	// antes de ir a Scrydex: si la expansión no tiene nada listado se salta sin
	// gastar la llamada, y si tiene, solo esas cartas se refrescan.
	ListedExternalIDs(ctx context.Context, gameCode, expansionExternalID string) (map[string]bool, error)

	// RefreshCardPrices actualiza el precio de mercado de las variantes de la
	// carta que ya existen y devuelve si encontró la carta. Si la carta o la
	// variante no están en la base no inserta nada y devuelve false: el precio
	// de una carta que nadie vende no interesa.
	RefreshCardPrices(ctx context.Context, gameCode string, card catalog.Card) (bool, error)
}
