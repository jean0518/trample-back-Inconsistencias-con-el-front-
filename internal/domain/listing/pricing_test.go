package listing

import (
	"testing"
)

func TestStandardizedPriceCOP(t *testing.T) {
	cases := []struct {
		name string
		usd  float64
		trm  float64
		want float64
	}{
		{"menor a 1 USD usa precio fijo", 0.50, 4000, 2000},
		{"justo debajo del umbral", 0.99, 4000, 2000},
		{"independiente de la TRM", 0.20, 9000, 2000},
		{"umbral exacto se convierte normal", 1.00, 4000, 4000},
		{"multiplo exacto no cambia", 2.50, 4000, 10000},
		{"redondea hacia arriba al millar", 2.61, 4000, 11000},
		{"ejemplo 3.40 USD con TRM 3072.647", 3.40, 3072.647, 11000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := StandardizedPriceCOP(tc.usd, tc.trm); got != tc.want {
				t.Fatalf("StandardizedPriceCOP(%v, %v) = %v, want %v", tc.usd, tc.trm, got, tc.want)
			}
		})
	}
}

// TestIsDiscountedLanguage fija la regla de negocio: el único idioma con precio
// propio es el español de Pokémon. Es la fuente única de esa decisión, así que
// este test es el que avisa si alguna de las rutas que lo consultan —el re-preciado
// de listings, el precio por idioma y los avisos al staff— se separa del resto.
func TestIsDiscountedLanguage(t *testing.T) {
	for _, c := range []struct{ game, language string }{
		{"pokemon", "Español"},
		{"pokemon", "Spanish"},
	} {
		if !IsDiscountedLanguage(c.game, c.language) {
			t.Errorf("IsDiscountedLanguage(%q, %q) = false, want true", c.game, c.language)
		}
	}

	sinDescuento := []struct{ game, language string }{
		// El español de los otros juegos va al precio de mercado.
		{"mtg", "Español"},
		{"mtg", "Spanish"},
		{"riftbound", "Español"},
		// Y ningún otro idioma de Pokémon.
		{"pokemon", "Inglés"},
		{"pokemon", "Japonés"},
		{"pokemon", "Coreano"},
		{"pokemon", "Chinese Simplified"},
		{"pokemon", ""},
		{"", "Español"},
	}
	for _, c := range sinDescuento {
		if IsDiscountedLanguage(c.game, c.language) {
			t.Errorf("IsDiscountedLanguage(%q, %q) = true, want false", c.game, c.language)
		}
	}
}

// El precio en español sale del 80 % del USD en inglés, y el COP se calcula
// desde ese USD con la regla normal, no descontando el COP ya redondeado.
func TestPrecioEspanolDesdeIngles(t *testing.T) {
	cases := []struct {
		name    string
		usd     float64
		trm     float64
		wantUSD float64
		wantCOP float64
	}{
		{"redondea al millar despues del descuento", 2.61, 4000, 2.09, 9000},
		{"respeta el minimo de 2.000", 1.10, 4000, 0.88, 2000},
		{"multiplo exacto", 5.00, 4000, 4.00, 16000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			usd := SpanishPriceUSD(tc.usd)
			if usd != tc.wantUSD {
				t.Fatalf("SpanishPriceUSD(%v) = %v, want %v", tc.usd, usd, tc.wantUSD)
			}
			if cop := StandardizedPriceCOP(usd, tc.trm); cop != tc.wantCOP {
				t.Fatalf("COP = %v, want %v", cop, tc.wantCOP)
			}
		})
	}
}
