package listing

import "math"

// Reglas de precio de venta en COP:
//
//   - Precio menor a 1 USD: valor fijo de 2.000 COP.
//   - Resto: conversión USD→COP con la TRM del día, redondeada hacia arriba
//     al siguiente múltiplo de 1.000 (ej: 10.447 ⇒ 11.000) para que el valor
//     mostrado sea fijo y fácil de leer.
const (
	FixedPriceThresholdUSD = 1.0
	FixedPriceUnderUSDCOP  = 2000.0
	PriceRoundingStepCOP   = 1000.0
)

// StandardizedPriceCOP aplica las reglas de precio sobre un valor en USD
// convertido con la TRM entregada.
func StandardizedPriceCOP(priceUSD, trm float64) float64 {
	if priceUSD < FixedPriceThresholdUSD {
		return FixedPriceUnderUSDCOP
	}
	cop := priceUSD * trm
	return math.Ceil(cop/PriceRoundingStepCOP) * PriceRoundingStepCOP
}

// SpanishPriceFactor es la fracción del precio en inglés a la que se venden las
// cartas en español. Scrydex no indexa cartas en español, así que su precio
// siempre se deriva del de la versión en inglés.
//
// El descuento es solo de Pokémon, y solo del español: es la única combinación
// en la que el precio Published difiere del de mercado. Cualquier otro idioma,
// en cualquier juego, se publica al precio de mercado de la impresión, que es el
// que el webhook de precios mantiene al día. Ver IsDiscountedLanguage.
const SpanishPriceFactor = 0.80

// SpanishPriceUSD es el precio en USD de la carta en español a partir del precio
// en inglés, redondeado a centavos. El precio en COP se saca de este valor con
// StandardizedPriceCOP, igual que cualquier otro listing.
func SpanishPriceUSD(englishUSD float64) float64 {
	return math.Round(englishUSD*SpanishPriceFactor*100) / 100
}

// IsSpanishLanguage indica si un nombre de idioma corresponde al español. Acepta
// tanto la etiqueta en español ("Español", que es la que se persiste en
// inventory_listings.language) como la de Scrydex ("Spanish", que es la que
// envía el front al endpoint de precio por idioma).
//
// Es el equivalente en Go del predicado `language IN ('Spanish', 'Español')` que
// usa el SQL de re-preciado: ambos lados deben seguir aceptando las mismas
// etiquetas o una carta quedaría a un precio y la otra al descontado.
func IsSpanishLanguage(name string) bool {
	return name == "Spanish" || name == "Español"
}

// IsDiscountedLanguage indica si un listing en ese idioma se publica al precio
// descontado en vez de al de mercado. Hoy la única combinación que lo hace es
// Pokémon en español.
//
// Es la fuente única de esa regla. La consultan el re-preciado de listings al
// refrescar precios, el endpoint de precio por idioma y los avisos al staff, y
// todas tienen que coincidir: si una discounted y otra no, la misma carta
// aparecería a dos precios distintos según por dónde se mire.
//
// El idioma no se busca en Scrydex para nada más: el de cualquier otra impresión
// es el de mercado de esa impresión, que es el mismo que trae el webhook.
func IsDiscountedLanguage(gameCode, language string) bool {
	return gameCode == "pokemon" && IsSpanishLanguage(language)
}
