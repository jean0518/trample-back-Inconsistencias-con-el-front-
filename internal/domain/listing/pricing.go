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
