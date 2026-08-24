package catalog

import (
	"context"
	"testing"

	"trample-back/internal/domain/catalog"
	"trample-back/internal/ports/out"
)

type fakeCardRepo struct {
	out.CardRepository
	synced map[string]bool
}

func newFakeCardRepo() *fakeCardRepo {
	return &fakeCardRepo{synced: make(map[string]bool)}
}

func (f *fakeCardRepo) SyncCard(_ context.Context, gameCode string, card catalog.Card) error {
	f.synced[gameCode+":"+card.ExternalID] = true
	return nil
}

func cardWithID(externalID string) catalog.Card {
	return catalog.Card{ExternalID: externalID, Name: externalID}
}

// loadCache siembra búsquedas en la caché en memoria sin tocar Scrydex.
func newUseCaseWithCache(cache map[string]cachedSearch) (*ImportCardUseCase, *fakeCardRepo) {
	uc := &ImportCardUseCase{search: NewSearchScrydex(nil, nil), repo: nil}
	for id, c := range cache {
		uc.search.store(id, c.GameCode, c.Cards)
	}
	repo := newFakeCardRepo()
	uc.repo = repo
	return uc, repo
}

func TestImportByGroupsMultiplesBusquedas(t *testing.T) {
	uc, repo := newUseCaseWithCache(map[string]cachedSearch{
		"search_pokemon": {GameCode: "pokemon", Cards: []catalog.Card{
			cardWithID("sv3-180"), cardWithID("sv3-181"),
		}},
		"search_mtg": {GameCode: "mtg", Cards: []catalog.Card{
			cardWithID("mh3-100"),
		}},
	})

	cards, err := uc.ImportByGroups(context.Background(), []ImportGroup{
		{SearchID: "search_pokemon", ExternalIDs: []string{"sv3-180", "sv3-181"}},
		{SearchID: "search_mtg", ExternalIDs: []string{"mh3-100"}},
	})
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	if len(cards) != 3 {
		t.Fatalf("esperaba 3 cartas importadas, hay %d", len(cards))
	}
	if len(repo.synced) != 3 {
		t.Fatalf("esperaba 3 SyncCard, hubo %d", len(repo.synced))
	}
}

func TestImportByGroupsDeduplicaEntreGrupos(t *testing.T) {
	same := cardWithID("dup-1")
	uc, repo := newUseCaseWithCache(map[string]cachedSearch{
		"s1": {GameCode: "pokemon", Cards: []catalog.Card{same, cardWithID("other-9")}},
		"s2": {GameCode: "pokemon", Cards: []catalog.Card{cardWithID("dup-1")}},
	})

	cards, err := uc.ImportByGroups(context.Background(), []ImportGroup{
		{SearchID: "s1", ExternalIDs: []string{"dup-1", "other-9"}},
		{SearchID: "s2", ExternalIDs: []string{"dup-1"}},
	})
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	if len(cards) != 2 {
		t.Fatalf("esperaba 2 cartas (dedupe), hay %d", len(cards))
	}
	if len(repo.synced) != 2 {
		t.Fatalf("SyncCard duplicado para dup-1: %v", repo.synced)
	}
}

func TestImportByGroupsMismoExternalIDDistintoJuegoNoSeDeduplica(t *testing.T) {
	uc, repo := newUseCaseWithCache(map[string]cachedSearch{
		"s1": {GameCode: "pokemon", Cards: []catalog.Card{cardWithID("x-1")}},
		"s2": {GameCode: "mtg", Cards: []catalog.Card{cardWithID("x-1")}},
	})

	cards, err := uc.ImportByGroups(context.Background(), []ImportGroup{
		{SearchID: "s1", ExternalIDs: []string{"x-1"}},
		{SearchID: "s2", ExternalIDs: []string{"x-1"}},
	})
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	if len(cards) != 2 || len(repo.synced) != 2 {
		t.Fatalf("cartas de juegos distintos no deben deduplicarse: %d/%d", len(cards), len(repo.synced))
	}
}

func TestImportByGroupsBusquedaExpirada(t *testing.T) {
	uc, _ := newUseCaseWithCache(map[string]cachedSearch{
		"s1": {GameCode: "pokemon", Cards: []catalog.Card{cardWithID("a-1")}},
	})

	_, err := uc.ImportByGroups(context.Background(), []ImportGroup{
		{SearchID: "inexistente", ExternalIDs: []string{"a-1"}},
	})
	if err == nil {
		t.Fatal("esperaba error por búsqueda expirada/inexistente")
	}
}

func TestImportByGroupsSinSeleccionEnResultados(t *testing.T) {
	uc, _ := newUseCaseWithCache(map[string]cachedSearch{
		"s1": {GameCode: "pokemon", Cards: []catalog.Card{cardWithID("a-1")}},
	})

	_, err := uc.ImportByGroups(context.Background(), []ImportGroup{
		{SearchID: "s1", ExternalIDs: []string{"no-existe"}},
	})
	if err == nil {
		t.Fatal("esperaba error cuando ninguna seleccionada está en resultados")
	}
}

func TestImportByGroupsValidacionEntradas(t *testing.T) {
	uc, _ := newUseCaseWithCache(nil)

	if _, err := uc.ImportByGroups(context.Background(), nil); err == nil {
		t.Fatal("esperaba error con groups vacío")
	}
	if _, err := uc.ImportByGroups(context.Background(), []ImportGroup{{SearchID: "", ExternalIDs: []string{"a"}}}); err == nil {
		t.Fatal("esperaba error sin search_id")
	}
	if _, err := uc.ImportByGroups(context.Background(), []ImportGroup{{SearchID: "s1"}}); err == nil {
		t.Fatal("esperaba error sin external_ids")
	}
}
