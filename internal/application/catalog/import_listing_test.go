package catalog

import (
	"context"
	"testing"

	"trample-back/internal/domain/catalog"
	"trample-back/internal/domain/listing"
	"trample-back/internal/ports/out"
)

type fakeCardRepoVariant struct {
	fakeCardRepo
	variantID int64
}

func (f *fakeCardRepoVariant) GetVariantID(_ context.Context, _, _, _ string) (int64, error) {
	return f.variantID, nil
}

type fakeListingRepo struct {
	out.ListingRepository
	inputs   []listing.CreateInput
	added    []listing.UpdateStockInput
	nextID   int64
	existing map[int64]listing.Listing
}

func (f *fakeListingRepo) Create(_ context.Context, input listing.CreateInput) (listing.Listing, error) {
	f.nextID++
	f.inputs = append(f.inputs, input)
	return listing.Listing{
		ID:        f.nextID,
		SellerID:  input.SellerID,
		VariantID: input.VariantID,
		Quantity:  input.Quantity,
		PriceUSD:  input.PriceUSD,
		PriceCOP:  input.PriceCOP,
		Status:    "active",
	}, nil
}

func (f *fakeListingRepo) FindBySellerAndVariant(_ context.Context, _, variantID int64) (listing.Listing, error) {
	if l, ok := f.existing[variantID]; ok {
		return l, nil
	}
	return listing.Listing{}, listing.ErrNotFound
}

func (f *fakeListingRepo) AddQuantity(_ context.Context, input listing.UpdateStockInput) (listing.Listing, error) {
	f.added = append(f.added, input)
	prev := f.existing[0]
	return listing.Listing{
		ID:       input.ID,
		SellerID: input.SellerID,
		Quantity: prev.Quantity + input.Quantity,
		Status:   "active",
	}, nil
}

type fakeTRM struct{ rate float64 }

func (f fakeTRM) GetRate(context.Context) (float64, error) { return f.rate, nil }

func setupListingUC(cache map[string]cachedSearch, rate float64) (*ImportListingUseCase, *fakeCardRepoVariant, *fakeListingRepo) {
	uc := &ImportListingUseCase{}
	uc.search = NewSearchScrydex(nil, nil)
	for id, c := range cache {
		uc.search.store(id, c.GameCode, c.Cards)
	}
	cards := &fakeCardRepoVariant{fakeCardRepo: fakeCardRepo{synced: make(map[string]bool)}, variantID: 42}
	listings := &fakeListingRepo{existing: make(map[int64]listing.Listing)}
	uc.cards = cards
	uc.listings = listings
	uc.trm = fakeTRM{rate: rate}
	return uc, cards, listings
}

func cardWithVariants(externalID, variantName string, marketUSD float64) catalog.Card {
	c := cardWithID(externalID)
	if variantName != "" {
		v := catalog.Variant{Name: variantName}
		if marketUSD > 0 {
			v.NMPrice = &catalog.Price{MarketUSD: marketUSD}
		}
		c.Variants = []catalog.Variant{v}
	}
	return c
}

func TestImportListingCreaListingsConDefaults(t *testing.T) {
	uc, cards, listings := setupListingUC(map[string]cachedSearch{
		"s1": {GameCode: "pokemon", Cards: []catalog.Card{
			cardWithVariants("sv3-180", "Normal", 1.50),
			cardWithVariants("sv3-181", "Holo", 4.00),
		}},
	}, 4000)

	result, err := uc.Execute(context.Background(), 7, []ImportListingGroup{
		{SearchID: "s1", Items: []ListingItem{
			{ExternalID: "sv3-180", Quantity: 5},
			{ExternalID: "sv3-181", Quantity: 2, Language: "Español"},
		}},
	})
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	if len(result) != 2 || len(listings.inputs) != 2 {
		t.Fatalf("esperaba 2 listings, hay %d", len(result))
	}
	first := listings.inputs[0]
	if first.SellerID != 7 || first.Quantity != 5 || first.Language != "Inglés" {
		t.Fatalf("defaults incorrectos en listing 1: %+v", first)
	}
	if first.PriceUSD != 1.50 {
		t.Fatalf("esperaba precio de mercado 1.50, hay %f", first.PriceUSD)
	}
	if listings.inputs[1].Language != "Español" {
		t.Fatalf("idioma explícito no respetado: %+v", listings.inputs[1])
	}
	if result[0].PriceCOP != 6000 {
		t.Fatalf("esperaba COP 6000 (1.50*4000), hay %f", result[0].PriceCOP)
	}
	if !cards.synced["pokemon:sv3-180"] {
		t.Fatal("la carta debió sincronizarse antes del listing")
	}
	if listings.inputs[0].VariantID != 42 {
		t.Fatalf("variant_id inesperado: %d", listings.inputs[0].VariantID)
	}
}

func TestImportListingPrecioManualTienePrioridad(t *testing.T) {
	uc, _, listings := setupListingUC(map[string]cachedSearch{
		"s1": {GameCode: "pokemon", Cards: []catalog.Card{
			cardWithVariants("sv3-180", "Normal", 1.50),
		}},
	}, 4000)

	manual := 9.99
	_, err := uc.Execute(context.Background(), 7, []ImportListingGroup{
		{SearchID: "s1", Items: []ListingItem{{ExternalID: "sv3-180", Quantity: 1, PriceUSD: &manual}}},
	})
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	if listings.inputs[0].PriceUSD != 9.99 {
		t.Fatalf("precio manual no aplicado: %f", listings.inputs[0].PriceUSD)
	}
}

func TestImportListingSinPrecioDisponibleFalla(t *testing.T) {
	uc, _, _ := setupListingUC(map[string]cachedSearch{
		"s1": {GameCode: "pokemon", Cards: []catalog.Card{
			cardWithVariants("sv3-180", "Normal", 0),
		}},
	}, 4000)

	if _, err := uc.Execute(context.Background(), 7, []ImportListingGroup{
		{SearchID: "s1", Items: []ListingItem{{ExternalID: "sv3-180", Quantity: 1}}},
	}); err == nil {
		t.Fatal("esperaba error cuando no hay precio de mercado ni price_usd")
	}
}

func TestImportListingDuplicadosSeOmiten(t *testing.T) {
	same := cardWithVariants("dup-1", "Normal", 1.00)
	uc, _, listings := setupListingUC(map[string]cachedSearch{
		"s1": {GameCode: "pokemon", Cards: []catalog.Card{same}},
		"s2": {GameCode: "pokemon", Cards: []catalog.Card{cardWithVariants("dup-1", "Normal", 2.00)}},
	}, 4000)

	result, err := uc.Execute(context.Background(), 7, []ImportListingGroup{
		{SearchID: "s1", Items: []ListingItem{{ExternalID: "dup-1", Quantity: 1}}},
		{SearchID: "s2", Items: []ListingItem{{ExternalID: "dup-1", Quantity: 3}}},
	})
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	if len(result) != 1 || len(listings.inputs) != 1 {
		t.Fatalf("el duplicado debió omitirse: %d listings", len(result))
	}
	if listings.inputs[0].Quantity != 1 {
		t.Fatal("debe ganar la primera aparición")
	}
}

func TestImportListingSumaStockSiYaExiste(t *testing.T) {
	uc, _, listings := setupListingUC(map[string]cachedSearch{
		"s1": {GameCode: "riftbound", Cards: []catalog.Card{
			cardWithVariants("diana", "Normal", 0.20),
		}},
	}, 4000)
	listings.existing[42] = listing.Listing{
		ID: 99, SellerID: 7, VariantID: 42,
		Quantity: 4, PriceUSD: 0.25, PriceCOP: 1000, Status: "active",
	}

	result, err := uc.Execute(context.Background(), 7, []ImportListingGroup{
		{SearchID: "s1", Items: []ListingItem{{ExternalID: "diana", Quantity: 3}}},
	})
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	if len(listings.inputs) != 0 {
		t.Fatalf("no debía crearse un listing nuevo, se crearon %d", len(listings.inputs))
	}
	if len(listings.added) != 1 || listings.added[0].ID != 99 || listings.added[0].Quantity != 3 {
		t.Fatalf("merge incorrecto: %+v", listings.added)
	}
	if len(result) != 1 || !result[0].Merged {
		t.Fatalf("respuesta debía marcar merged=true: %+v", result)
	}
}

func TestImportListingValidaciones(t *testing.T) {
	uc, _, _ := setupListingUC(map[string]cachedSearch{
		"s1": {GameCode: "pokemon", Cards: []catalog.Card{
			cardWithVariants("sv3-180", "Normal", 1.50),
		}},
	}, 4000)

	if _, err := uc.Execute(context.Background(), 7, nil); err == nil {
		t.Fatal("esperaba error sin grupos")
	}
	if _, err := uc.Execute(context.Background(), 0, []ImportListingGroup{
		{SearchID: "s1", Items: []ListingItem{{ExternalID: "sv3-180", Quantity: 1}}},
	}); err == nil {
		t.Fatal("esperaba error con seller_id inválido")
	}
	if _, err := uc.Execute(context.Background(), 7, []ImportListingGroup{
		{SearchID: "s1", Items: []ListingItem{{ExternalID: "sv3-180", Quantity: 0}}},
	}); err == nil {
		t.Fatal("esperaba error con cantidad 0")
	}
	if _, err := uc.Execute(context.Background(), 7, []ImportListingGroup{
		{SearchID: "s1", Items: []ListingItem{{ExternalID: "no-existe", Quantity: 1}}},
	}); err == nil {
		t.Fatal("esperaba error con carta ajena a la búsqueda")
	}
}
