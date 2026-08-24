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
