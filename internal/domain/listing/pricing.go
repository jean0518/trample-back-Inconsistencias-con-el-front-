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
// Es la misma regla que aplica el re-preciado de listings al refrescar precios
// desde Scrydex (ver repriceListings en card_repository_pg.go) y la que usa el
// endpoint de precio por idioma (PriceByLanguageUseCase), así que vive acá para
// que ambas rutas no se desincronicen.
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
