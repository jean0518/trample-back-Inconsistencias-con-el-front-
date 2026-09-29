package catalog

import (
	"context"
	"testing"

	"trample-back/internal/domain/catalog"
	"trample-back/internal/ports/out"
)

// fakeSearchScrydex registra los SearchParams con los que lo llamaron.
type fakeSearchScrydex struct {
	out.ScrydexClient
	got   out.SearchParams
	calls int
}

func (f *fakeSearchScrydex) SearchCards(_ context.Context, p out.SearchParams) ([]catalog.Card, error) {
	f.got = p
	f.calls++
	return []catalog.Card{{ExternalID: "me55-158", Name: "Mew ex"}}, nil
}

// TestSearchAceptaSoloExternalID cubre que el filtro por ID no quede muerto: con
// `id:` la impresión queda determinada y el nombre no aporta, así que exigirlo
// haría inalcanzable el filtro por ID, que es justo lo que evita que "Mew ex"
// devuelva la impresión #66 en vez de la #158.
func TestSearchAceptaSoloExternalID(t *testing.T) {
	scrydex := &fakeSearchScrydex{}
	uc := NewSearchScrydex(scrydex, fakeTRM{rate: testTRM})

	if _, err := uc.Search(t.Context(), out.SearchParams{
		GameCode:   "pokemon",
		ExternalID: "me55-158",
	}); err != nil {
		t.Fatalf("una búsqueda solo por external_id debe ser válida: %v", err)
	}
	if scrydex.calls != 1 {
		t.Fatalf("llamó a Scrydex %d veces, want 1", scrydex.calls)
	}
	if scrydex.got.ExternalID != "me55-158" {
		t.Fatalf("no propagó external_id a Scrydex: %+v", scrydex.got)
	}
}

// TestSearchSigueAceptandoSoloNombre evita que el relajamiento de la validación
// rompa el flujo de búsqueda del panel, que no manda ID.
func TestSearchSigueAceptandoSoloNombre(t *testing.T) {
	scrydex := &fakeSearchScrydex{}
	uc := NewSearchScrydex(scrydex, fakeTRM{rate: testTRM})

	if _, err := uc.Search(t.Context(), out.SearchParams{
		GameCode: "pokemon",
		Name:     "Mew ex",
	}); err != nil {
		t.Fatalf("una búsqueda solo por nombre debe seguir siendo válida: %v", err)
	}
	if scrydex.got.ExternalID != "" {
		t.Fatalf("no debe inventar un external_id: %q", scrydex.got.ExternalID)
	}
}

func TestSearchExigeNameOExternalID(t *testing.T) {
	scrydex := &fakeSearchScrydex{}
	uc := NewSearchScrydex(scrydex, fakeTRM{rate: testTRM})

	// Sin game_code, ni con los dos filtros, hay error.
	for _, p := range []out.SearchParams{
		{Name: "Mew ex"},
		{ExternalID: "me55-158"},
		{},
	} {
		if _, err := uc.Search(t.Context(), p); err == nil {
			t.Fatalf("se esperaba error para %+v", p)
		}
	}
	if scrydex.calls != 0 {
		t.Fatalf("no debe llamar a Scrydex cuando la petición es inválida, llamó %d veces", scrydex.calls)
	}
}
