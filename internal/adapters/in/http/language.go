package http

import (
	"math"

	"trample-back/internal/domain/catalog"
)

// languageNameToCode convierte el nombre completo de un idioma (en inglés,
// tal como lo envía el front) al código que usa Scrydex en su campo
// language_code (p. ej. "JA", "EN").
// Devuelve "" si el idioma no está mapeado.
func languageNameToCode(name string) string {
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
	}
	return codes[name]
}

// isSpanish reporta si el nombre del idioma (enviado por el front en inglés o
// español) corresponde al español. Scrydex no indexa cartas en español, así
// que se trata como caso especial: precio en inglés × factor.
func isSpanish(name string) bool {
	return name == "Spanish" || name == "Español"
}

// applyPriceDiscount devuelve una copia de la carta con todos los precios de
// sus variantes multiplicados por factor (p. ej. 0.80 para el 80 %).
func applyPriceDiscount(card catalog.Card, factor float64) catalog.Card {
	for i, v := range card.Variants {
		if v.NMPrice == nil {
			continue
		}
		p := *v.NMPrice
		p.MarketUSD = math.Round(p.MarketUSD*factor*100) / 100
		p.LowUSD = math.Round(p.LowUSD*factor*100) / 100
		p.MarketCOP = int64(math.Round(float64(p.MarketCOP) * factor))
		p.LowCOP = int64(math.Round(float64(p.LowCOP) * factor))
		card.Variants[i].NMPrice = &p
	}
	return card
}
