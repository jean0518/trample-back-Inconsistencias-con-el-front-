package product

import (
	"strings"
	"testing"
)

func TestInputValidate(t *testing.T) {
	ok := Input{Title: "  Caja de sobres Pokémon  ", Description: " Display 36 sobres ", PriceCOP: 450000, Stock: 3}

	got, err := ok.Validate()
	if err != nil {
		t.Fatalf("entrada válida: %v", err)
	}
	if got.Title != "Caja de sobres Pokémon" || got.Description != "Display 36 sobres" {
		t.Errorf("no recortó los textos: %q / %q", got.Title, got.Description)
	}

	cases := map[string]Input{
		"título vacío":        {Title: "   ", PriceCOP: 1000, Stock: 1},
		"título largo":        {Title: strings.Repeat("a", MaxTitleLength+1), PriceCOP: 1000},
		"descripción larga":   {Title: "x", Description: strings.Repeat("a", MaxDescriptionLength+1), PriceCOP: 1000},
		"precio cero":         {Title: "x", PriceCOP: 0},
		"precio negativo":     {Title: "x", PriceCOP: -5},
		"precio enorme":       {Title: "x", PriceCOP: MaxPriceCOP + 1},
		"stock negativo":      {Title: "x", PriceCOP: 1000, Stock: -1},
		"stock sobre el tope": {Title: "x", PriceCOP: 1000, Stock: MaxStock + 1},
	}
	for name, in := range cases {
		if _, err := in.Validate(); err == nil {
			t.Errorf("%s: esperaba error", name)
		}
	}

	if _, err := (Input{Title: "x", PriceCOP: 1000, Stock: 0}).Validate(); err != nil {
		t.Errorf("stock 0 es válido (producto agotado): %v", err)
	}
}

func TestAvailabilityAndSoldOut(t *testing.T) {
	cases := []struct {
		stock, reserved, available int
		soldOut                    bool
	}{
		{5, 0, 5, false},
		{5, 2, 3, false},
		{5, 5, 0, true},
		{0, 0, 0, true},
		{2, 4, 0, true}, // nunca negativo aunque haya más reservas que stock
	}
	for _, c := range cases {
		p := Product{Stock: c.stock, Reserved: c.reserved}
		if p.Available() != c.available || p.SoldOut() != c.soldOut {
			t.Errorf("stock=%d reserved=%d: available=%d soldOut=%v", c.stock, c.reserved, p.Available(), p.SoldOut())
		}
	}
}

func TestListFilterNormalize(t *testing.T) {
	f := ListFilter{Query: "  caja ", Limit: 0, Offset: -3}.Normalize()
	if f.Query != "caja" || f.Limit != DefaultListLimit || f.Offset != 0 {
		t.Errorf("normalización: %+v", f)
	}
	if got := (ListFilter{Limit: 9999}).Normalize().Limit; got != MaxListLimit {
		t.Errorf("limit sin tope: %d", got)
	}
}
