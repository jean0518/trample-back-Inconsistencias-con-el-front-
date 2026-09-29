package telegram

import (
	"strings"
	"testing"

	"trample-back/internal/domain/catalog"
)

func TestFormatCOP(t *testing.T) {
	cases := map[int64]string{0: "$0", 900: "$900", 2000: "$2.000", 50000: "$50.000", 1234567: "$1.234.567"}
	for in, want := range cases {
		if got := formatCOP(in); got != want {
			t.Errorf("formatCOP(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildMessagesEscapaYParte(t *testing.T) {
	change := catalog.CardPriceChange{
		ExpansionID: "MH3",
		CardName:    "Ugin's <Labyrinth> & co",
		Update:      catalog.PriceUpdate{VariantName: "foil", HadPrice: true, OldUSD: 10.2, NewUSD: 12.5, NewCOP: 50000, ListingsRepriced: 2},
	}
	msgs := buildMessages(catalog.PriceReport{GameCode: "mtg", Changes: []catalog.CardPriceChange{change}})
	if len(msgs) != 1 {
		t.Fatalf("se esperaba 1 mensaje, hubo %d", len(msgs))
	}
	for _, want := range []string{"Magic", "&lt;Labyrinth&gt; &amp; co", "US$10.20 → US$12.50", "$50.000 COP", "Español: US$10.00", "2 listings"} {
		if !strings.Contains(msgs[0], want) {
			t.Errorf("el mensaje no contiene %q:\n%s", want, msgs[0])
		}
	}

	many := make([]catalog.CardPriceChange, 100)
	for i := range many {
		many[i] = change
	}
	msgs = buildMessages(catalog.PriceReport{GameCode: "mtg", Changes: many})
	if len(msgs) < 2 {
		t.Fatalf("100 cartas deberían partirse en varios mensajes, hubo %d", len(msgs))
	}
	for _, m := range msgs {
		if len(m) > 4096 {
			t.Errorf("mensaje de %d caracteres supera el límite de Telegram", len(m))
		}
	}
}

func TestBuildMessagesListaExpansionesSaltadas(t *testing.T) {
	report := catalog.PriceReport{
		GameCode: "mtg",
		Skipped: []catalog.SkippedExpansion{
			{ID: "SLD", Reason: catalog.SkipNotInDB},
			{ID: "MH3", Name: "Modern Horizons 3", Reason: catalog.SkipNoStock},
			{ID: "BRO", Reason: catalog.SkipScrydexError},
		},
	}
	msgs := buildMessages(report)
	if len(msgs) != 1 {
		t.Fatalf("se esperaba 1 mensaje, hubo %d", len(msgs))
	}
	for _, want := range []string{
		"Ningún precio de tu inventario cambió",
		"Expansiones no procesadas (3)",
		"No está en la base (1)", "<code>SLD</code>",
		"Sin listings activos con stock (1)", "Modern Horizons 3 (<code>MH3</code>)",
		"Error al consultar Scrydex (1)", "<code>BRO</code>",
	} {
		if !strings.Contains(msgs[0], want) {
			t.Errorf("el mensaje no contiene %q:\n%s", want, msgs[0])
		}
	}
}
