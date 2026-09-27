package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"trample-back/internal/domain/catalog"
	"trample-back/internal/domain/listing"
	"trample-back/internal/domain/owner"
	"trample-back/internal/ports/out"
)

const (
	testGameCode = "pokemon"
	testRate     = 4000.0
)

// fakeManualCardRepo simula el alta de la carta. Solo este caso de uso necesita
// UpsertManualCard, así que el doble implementa únicamente ese método.
type fakeManualCardRepo struct {
	out.ManualCardRepository
	got    catalog.ManualCard
	result catalog.ManualCardResult
	calls  int
}

func (f *fakeManualCardRepo) UpsertManualCard(_ context.Context, card catalog.ManualCard) (catalog.ManualCardResult, error) {
	f.calls++
	f.got = card
	return f.result, nil
}

// fakeImageRepo registra qué se enlazó para poder comprobar que la imagen se
// guarda contra la variante recién creada.
type fakeImageRepo struct {
	out.CardImageRepository
	storedImageID int64
	err           error
	storeCalls    int
	linkCalls     int
	lastCard      int64
	lastLinkedURL string
}

func (f *fakeImageRepo) Store(_ context.Context, cardID int64, _ catalog.ImageUpload) (string, error) {
	f.storeCalls++
	f.lastCard = cardID
	if f.err != nil {
		return "", f.err
	}
	return fmt.Sprintf("/card-images/%d", f.storedImageID), nil
}

func (f *fakeImageRepo) Link(_ context.Context, cardID int64, externalURL string) error {
	f.linkCalls++
	f.lastCard = cardID
	f.lastLinkedURL = externalURL
	return f.err
}

// errTRM representa una TRM caída, que es el caso que debe abortar el alta
// antes de escribir nada.
type errTRM struct{ err error }

func (e errTRM) GetRate(context.Context) (float64, error) { return 0, e.err }

const (
	testCardID      = int64(42)
	testVariantID   = int64(99)
	testImageID     = int64(777)
	testDefaultOwnr = int64(1)
)

func newTestUseCase(t *testing.T) (*ManualListingUseCase, *fakeManualCardRepo, *fakeListingRepo, *fakeImageRepo) {
	t.Helper()
	cards := &fakeManualCardRepo{result: catalog.ManualCardResult{
		CardID:      testCardID,
		ExpansionID: 7,
		VariantID:   testVariantID,
		CardName:    "Pikachu ex",
		ExternalID:  "manual:pikachu-ex-4",
		VariantName: catalog.DefaultVariantName,
		CardCreated: true,
	}}
	listings := &fakeListingRepo{existing: make(map[int64]listing.Listing)}
	images := &fakeImageRepo{storedImageID: testImageID}
	owners := &fakeOwnerRepo{defaultOwner: owner.Owner{ID: testDefaultOwnr, Name: "trampleStore", IsDefault: true}}
	trm := fakeTRM{rate: testRate}

	return NewManualListingUseCase(cards, listings, owners, images, trm), cards, listings, images
}

func validInput() ManualListingInput {
	return ManualListingInput{
		GameCode:    testGameCode,
		ExpansionID: 7,
		Name:        "Pikachu ex",
		Number:      "4",
		Rarity:      "Raro",
		Quantity:    2,
		PriceUSD:    10,
	}
}

// pngBytes arma el mínimo de bytes que http.DetectContentType reconoce como
// PNG, que es como las pruebas simulan un archivo real sin meter un binario.
func pngBytes() []byte {
	return append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, make([]byte, 24)...)
}

func mustUpload(t *testing.T) catalog.ImageUpload {
	t.Helper()
	upload, err := catalog.NewImageUpload(pngBytes())
	if err != nil {
		t.Fatalf("el PNG de prueba debería ser válido: %v", err)
	}
	return upload
}

// --- pruebas ---

func TestExecuteCreaElListingCuandoNoHabiaUno(t *testing.T) {
	uc, _, listings, _ := newTestUseCase(t)

	got, err := uc.Execute(context.Background(), sellerUUID, validInput())
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}

	if len(listings.inputs) != 1 || len(listings.added) != 0 {
		t.Fatalf("debía crear un listing: create=%d add=%d", len(listings.inputs), len(listings.added))
	}
	if got.Merged {
		t.Fatal("no se fusionó nada: merged debe ser false")
	}
	if got.ListingID == 0 || got.CardID != testCardID {
		t.Fatalf("IDs inesperados: %+v", got)
	}
	// El precio en COP sale de la TRM del día, no de una conversión improvisada.
	if want := listing.StandardizedPriceCOP(10, testRate); got.PriceCOP != want {
		t.Fatalf("price_cop: esperaba %v, hay %v", want, got.PriceCOP)
	}
}

func TestExecuteSumaStockSiYaHabiaListing(t *testing.T) {
	uc, _, listings, _ := newTestUseCase(t)
	listings.existing[testVariantID] = listing.Listing{
		ID: 88, Quantity: 3, Language: DefaultListingLanguage, OwnerID: testDefaultOwnr, Status: "active",
	}

	got, err := uc.Execute(context.Background(), sellerUUID, validInput())
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}

	if len(listings.added) != 1 || len(listings.inputs) != 0 {
		t.Fatalf("debía sumar stock: create=%d add=%d", len(listings.inputs), len(listings.added))
	}
	if !got.Merged {
		t.Fatal("merged debe ser true cuando se reutilizó el listing")
	}
	// El listing queda con el stock acumulado, que es lo que ve el staff.
	if got.Quantity != 5 {
		t.Fatalf("esperaba 3+2=5 unidades, hay %d", got.Quantity)
	}
}

func TestExecuteUsaElPropietarioPorDefecto(t *testing.T) {
	uc, _, listings, _ := newTestUseCase(t)

	if _, err := uc.Execute(context.Background(), sellerUUID, validInput()); err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	if listings.inputs[0].OwnerID != testDefaultOwnr {
		t.Fatalf("esperaba el propietario por defecto %d, hay %d", testDefaultOwnr, listings.inputs[0].OwnerID)
	}
}

func TestExecuteRespetaElPropietarioIndicado(t *testing.T) {
	uc, _, listings, _ := newTestUseCase(t)

	in := validInput()
	in.OwnerID = 5
	if _, err := uc.Execute(context.Background(), sellerUUID, in); err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	if listings.inputs[0].OwnerID != 5 {
		t.Fatalf("esperaba el propietario 5, hay %d", listings.inputs[0].OwnerID)
	}
}

func TestExecuteRespetaElIdiomaIndicado(t *testing.T) {
	uc, _, listings, _ := newTestUseCase(t)

	in := validInput()
	in.Language = "Español"
	if _, err := uc.Execute(context.Background(), sellerUUID, in); err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	if listings.inputs[0].Language != "Español" {
		t.Fatalf("esperaba Español, hay %q", listings.inputs[0].Language)
	}
}

// Sin idioma el listing queda en el default, el mismo que aplica la base, para
// que el catálogo no muestre un idioma vacío.
func TestExecuteAplicaElIdiomaPorDefecto(t *testing.T) {
	uc, _, listings, _ := newTestUseCase(t)

	if _, err := uc.Execute(context.Background(), sellerUUID, validInput()); err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	if listings.inputs[0].Language != DefaultListingLanguage {
		t.Fatalf("esperaba %q, hay %q", DefaultListingLanguage, listings.inputs[0].Language)
	}
}

func TestExecuteRechazaCantidadInvalida(t *testing.T) {
	uc, _, _, _ := newTestUseCase(t)

	for _, qty := range []int{0, -1} {
		in := validInput()
		in.Quantity = qty
		if _, err := uc.Execute(context.Background(), sellerUUID, in); err == nil {
			t.Fatalf("la cantidad %d debería rechazarse", qty)
		}
	}
}

// Una carta a mano no tiene precio de mercado del que deducirlo, así que el
// precio es obligatorio en vez de opcional.
func TestExecuteExigePrecio(t *testing.T) {
	uc, _, _, _ := newTestUseCase(t)

	in := validInput()
	in.PriceUSD = 0
	_, err := uc.Execute(context.Background(), sellerUUID, in)
	if err == nil || !strings.Contains(err.Error(), "price_usd") {
		t.Fatalf("esperaba un error que pidiera price_usd, hay %v", err)
	}
}

func TestExecuteRechazaVendedorVacio(t *testing.T) {
	uc, cards, _, _ := newTestUseCase(t)

	if _, err := uc.Execute(context.Background(), "", validInput()); err == nil {
		t.Fatal("un seller vacío debería rechazarse antes de tocar la base")
	}
	if cards.calls != 0 {
		t.Fatal("no se debería haber llamado al repositorio de cartas")
	}
}

// Si la TRM falla no se debe haber escrito nada: la petición se rechaza antes de
// crear la carta.
func TestExecuteNoCreaNadaSiFallaLaTRM(t *testing.T) {
	cards := &fakeManualCardRepo{result: catalog.ManualCardResult{CardID: 1, VariantID: 2}}
	listings := &fakeListingRepo{existing: make(map[int64]listing.Listing)}
	owners := &fakeOwnerRepo{defaultOwner: owner.Owner{ID: 1, IsDefault: true}}
	uc := NewManualListingUseCase(cards, listings, owners, &fakeImageRepo{}, errTRM{err: errors.New("TRM no disponible")})

	if _, err := uc.Execute(context.Background(), sellerUUID, validInput()); err == nil {
		t.Fatal("se esperaba un error de TRM")
	}
	if cards.calls != 0 {
		t.Fatal("no debería haberse creado la carta si la TRM falló")
	}
}

func TestExecuteRechazaURLSinEsquema(t *testing.T) {
	uc, _, _, _ := newTestUseCase(t)

	in := validInput()
	in.ImageURL = "www.ejemplo.com/frontal.jpg"
	_, err := uc.Execute(context.Background(), sellerUUID, in)
	if err == nil || !strings.Contains(err.Error(), "http") {
		t.Fatalf("esperaba un error sobre el esquema, hay %v", err)
	}
}

func TestExecuteAceptaUnaURLValida(t *testing.T) {
	uc, _, _, images := newTestUseCase(t)

	in := validInput()
	in.ImageURL = "https://cdn.ejemplo.com/frontal.jpg"
	got, err := uc.Execute(context.Background(), sellerUUID, in)
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	if got.ImageURL != in.ImageURL {
		t.Fatalf("esperaba que la URL se conservara, hay %q", got.ImageURL)
	}
	// Una URL externa no se guarda en la base: ya vive en su hosting.
	if images.storeCalls != 0 {
		t.Fatal("una URL externa no debería pasar por el repositorio de imágenes")
	}
}

// Este es el punto 2: la imagen subida se guarda en la base y la respuesta
// devuelve la ruta del endpoint que la sirve.
func TestExecuteGuardaLaImagenSubida(t *testing.T) {
	uc, _, _, images := newTestUseCase(t)

	in := validInput()
	in.ImageFile = mustUpload(t)
	got, err := uc.Execute(context.Background(), sellerUUID, in)
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}

	if images.storeCalls != 1 {
		t.Fatalf("esperaba 1 llamada al repositorio de imágenes, hay %d", images.storeCalls)
	}
	if want := "/card-images/777"; got.ImageURL != want {
		t.Fatalf("esperaba %q, hay %q", want, got.ImageURL)
	}
	// Se enlaza a la carta recién creada, que es como el catálogo la resuelve.
	if images.lastCard != testCardID {
		t.Fatalf("imagen enlazada a card=%d, esperaba %d", images.lastCard, testCardID)
	}
}

// Una imagen por URL también tiene que quedar enlazada a la carta. Antes solo
// se guardaban los bytes subidos: la URL se validaba y se devolvía en la
// respuesta, pero la carta se quedaba sin frontal en el catálogo.
func TestExecuteEnlazaLaImagenPorURL(t *testing.T) {
	uc, _, _, images := newTestUseCase(t)

	const url = "https://cdn.ejemplo.com/frontal.jpg"
	in := validInput()
	in.ImageURL = url
	got, err := uc.Execute(context.Background(), sellerUUID, in)
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}

	if images.storeCalls != 0 {
		t.Fatalf("una URL externa no debe pasar por Store, hubo %d llamadas", images.storeCalls)
	}
	if images.linkCalls != 1 {
		t.Fatalf("esperaba 1 llamada a Link, hay %d", images.linkCalls)
	}
	if images.lastLinkedURL != url {
		t.Fatalf("se enlazó %q, esperaba %q", images.lastLinkedURL, url)
	}
	if images.lastCard != testCardID {
		t.Fatalf("imagen enlazada a card=%d, esperaba %d", images.lastCard, testCardID)
	}
	if got.ImageURL != url {
		t.Fatalf("la respuesta devolvió %q, esperaba %q", got.ImageURL, url)
	}
}

// Mandar las dos formas a la vez es el estado ambiguo que puede producir el
// formulario al cambiar de pestaña; se rechaza en vez de elegir una por gusto.
func TestExecuteRechazaURLYArchivoJuntos(t *testing.T) {
	uc, _, _, _ := newTestUseCase(t)

	in := validInput()
	in.ImageURL = "https://cdn.ejemplo.com/frontal.jpg"
	in.ImageFile = mustUpload(t)

	_, err := uc.Execute(context.Background(), sellerUUID, in)
	if err == nil || !strings.Contains(err.Error(), "no ambos") {
		t.Fatalf("esperaba un error de conflicto, hay %v", err)
	}
}

// Si la imagen falla después de crear el listing, el error debe ser reconocible
// con errors.Is para que el handler pueda responder 422 en vez de 400.
func TestExecuteMarcaElFalloDeImagenComoSentinel(t *testing.T) {
	uc, _, _, images := newTestUseCase(t)
	images.err = errors.New("disco lleno")

	in := validInput()
	in.ImageFile = mustUpload(t)

	_, err := uc.Execute(context.Background(), sellerUUID, in)
	if !errors.Is(err, ErrImageAttachment) {
		t.Fatalf("esperaba ErrImageAttachment, hay %v", err)
	}
	// El mensaje original debe sobrevivir para que el staff sepa qué pasó.
	if !strings.Contains(err.Error(), "disco lleno") {
		t.Fatalf("el error debía conservar la causa, hay %v", err)
	}
}

// La carta es lo primero que se resuelve, así que al repo le debe llegar ya
// normalizada y con el external_id derivado si el staff no envió uno.
func TestExecuteNormalizaLaCartaAntesDePersistir(t *testing.T) {
	uc, cards, _, _ := newTestUseCase(t)

	in := validInput()
	in.VariantName = "Regular"
	in.Name = "  Pikachu ex  "
	if _, err := uc.Execute(context.Background(), sellerUUID, in); err != nil {
		t.Fatalf("error inesperado: %v", err)
	}

	if cards.got.Name != "Pikachu ex" {
		t.Fatalf("esperaba el nombre recortado, hay %q", cards.got.Name)
	}
	if cards.got.VariantName != catalog.DefaultVariantName {
		t.Fatalf("\"Regular\" debe mapear a %q, hay %q", catalog.DefaultVariantName, cards.got.VariantName)
	}
	if cards.got.ExternalID != "manual:pikachu-ex-4" {
		t.Fatalf("esperaba el external_id derivado, hay %q", cards.got.ExternalID)
	}
}
