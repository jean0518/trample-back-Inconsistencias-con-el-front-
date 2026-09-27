package catalog

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"trample-back/internal/domain/catalog"
	"trample-back/internal/domain/listing"
	"trample-back/internal/domain/owner"
	"trample-back/internal/ports/out"
)

// DefaultListingLanguage es el idioma con el que queda una carta dada de alta sin
// que el staff indique otro. Coincide con el default de inventory_listings.
const DefaultListingLanguage = "Inglés"

// ErrImageAttachment indica que la carta se guardó pero el frontal no. Es un
// caso aparte de propósito: la petición debe responder 422 y no 400, porque
// reenviar el formulario con los mismos datos no lo arregla (hay que volver a
// elegir el archivo), aunque la carta exista y reenviar no la duplique.
//
// Se comprueba con errors.Is y no leyendo el texto del error, para que el
// handler no dependa de cómo esté redactado el mensaje.
var ErrImageAttachment = errors.New("la carta se guardó pero la imagen falló")

// ManualListingUseCase da de alta una carta en el inventario desde el panel.
//
// A diferencia de la importación por API, aquí el staff elige una expansión ya
// existente o crea una nueva, y puede aportar el frontal de dos formas
// intercambiables: la URL de un hosting externo o un archivo que sube desde el
// formulario. Ambas terminan en card_images, así que el catálogo las trata
// igual y el frontend no necesita saber de dónde salió cada una.
type ManualListingUseCase struct {
	cards    out.ManualCardRepository
	listings out.ListingRepository
	owners   out.OwnerRepository
	images   out.CardImageRepository
	trm      out.TRMClient
}

// NewManualListingUseCase construye el caso de uso.
func NewManualListingUseCase(
	cards out.ManualCardRepository,
	listings out.ListingRepository,
	owners out.OwnerRepository,
	images out.CardImageRepository,
	trm out.TRMClient,
) *ManualListingUseCase {
	return &ManualListingUseCase{cards: cards, listings: listings, owners: owners, images: images, trm: trm}
}

// ManualListingInput es el alta tal como llega del formulario, antes de
// validarse. Los punteros distinguen "no enviado" de "enviado en cero", que
// cambia el resultado: sin expansion_id se crea la expansión, y con
// price_usd ausente no se puede calcular el precio en COP.
type ManualListingInput struct {
	GameCode      string
	ExpansionID   int64
	ExpansionName string
	ExternalID    string
	Name          string
	Number        string
	Rarity        string
	VariantName   string
	Quantity      int
	PriceUSD      float64
	Language      string
	OwnerID       int64
	// ImageURL e ImageFile son excluyentes: el dominio rechaza que vengan las
	// dos informadas.
	ImageURL  string
	ImageFile catalog.ImageUpload
}

// ManualListingResult lleva los identificadores persistidos y la URL con la que
// quedó sirviéndose el frontal, para que el frontend pueda confirmar el alta y
// refrescar la ficha sin recargar el catálogo entero.
type ManualListingResult struct {
	ListingID   int64   `json:"listing_id"`
	CardID      int64   `json:"card_id"`
	CardName    string  `json:"card_name"`
	ExternalID  string  `json:"external_id"`
	VariantName string  `json:"variant_name"`
	ExpansionID int64   `json:"expansion_id"`
	Quantity    int     `json:"quantity"`
	PriceUSD    float64 `json:"price_usd"`
	PriceCOP    float64 `json:"price_cop"`
	Status      string  `json:"status"`
	// CardCreated indica que la carta no existía y se acaba de crear.
	CardCreated bool `json:"card_created"`
	// Merged indica que la carta ya tenía un listing de este vendedor con el
	// mismo idioma y propietario, y se le sumó stock en vez de crear otro.
	Merged bool `json:"merged"`
	// ImageURL es la ruta con la que se sirve el frontal, ya sea la que envió el
	// staff o el endpoint interno de la imagen subida.
	ImageURL string `json:"image_url,omitempty"`
}

// Execute valida, da de alta la carta, publica el stock y enlaza el frontal.
//
// El orden importa: la imagen necesita los IDs de carta y variante, que no
// existen hasta resolver el alta, y el precio en COP necesita la TRM.
//
// Si la imagen falla después de haber creado el listing, se devuelve
// ErrImageAttachment y el alta queda hecha: es preferible a devolver un error
// genérico que invite al staff a reenviar el formulario sin saber que la carta
// ya está. Y reenviar es seguro, porque el alta es idempotente por external_id
// y el stock se suma al listing existente.
func (uc *ManualListingUseCase) Execute(ctx context.Context, sellerID string, in ManualListingInput) (ManualListingResult, error) {
	if sellerID == "" {
		return ManualListingResult{}, errors.New("no se pudo identificar al usuario que registra la carta")
	}

	card, err := catalog.ManualCard{
		GameCode:      in.GameCode,
		ExpansionID:   in.ExpansionID,
		ExpansionName: in.ExpansionName,
		ExternalID:    in.ExternalID,
		Name:          in.Name,
		Number:        in.Number,
		Rarity:        in.Rarity,
		VariantName:   in.VariantName,
	}.Validate()
	if err != nil {
		return ManualListingResult{}, err
	}
	if in.Quantity < 1 {
		return ManualListingResult{}, errors.New("la cantidad debe ser al menos 1")
	}
	if in.PriceUSD <= 0 {
		return ManualListingResult{}, errors.New("indica price_usd: una carta a mano no tiene precio de mercado del que deducirlo")
	}

	imageSource, err := catalog.ValidateImageSource(in.ImageURL, in.ImageFile)
	if err != nil {
		return ManualListingResult{}, err
	}
	// Se comprueba la forma de la URL antes de tocar la base, para no dejar una
	// carta creada a la que después se le adjunte un frontal inalcanzable.
	if err := ValidateImageURL(imageSource.URL); err != nil {
		return ManualListingResult{}, err
	}

	language := strings.TrimSpace(in.Language)
	if language == "" {
		language = DefaultListingLanguage
	}
	ownerID := in.OwnerID
	if ownerID <= 0 {
		defaultOwner, err := uc.findDefaultOwner(ctx)
		if err != nil {
			return ManualListingResult{}, err
		}
		ownerID = defaultOwner.ID
	}

	// La TRM se pide antes de escribir nada: si falla, es preferible no haber
	// creado la carta todavía.
	rate, err := uc.trm.GetRate(ctx)
	if err != nil {
		return ManualListingResult{}, fmt.Errorf("obtener TRM: %w", err)
	}

	cardResult, err := uc.cards.UpsertManualCard(ctx, card)
	if err != nil {
		return ManualListingResult{}, err
	}

	// Si el vendedor ya tiene un listing vigente para esta variante con el
	// mismo idioma y propietario, se suma stock; si no, se crea uno nuevo con
	// stock separado. Misma regla que aplica la importación desde Scrydex.
	existing, findErr := uc.listings.FindBySellerAndVariantLanguageOwner(ctx, sellerID, cardResult.VariantID, language, ownerID)
	var (
		l      listing.Listing
		merged bool
	)
	switch {
	case findErr == nil:
		l, err = uc.listings.AddQuantity(ctx, listing.UpdateStockInput{
			ID:       existing.ID,
			SellerID: sellerID,
			Quantity: in.Quantity,
		})
		merged = true
	case errors.Is(findErr, listing.ErrNotFound):
		l, err = uc.listings.Create(ctx, listing.CreateInput{
			SellerID:  sellerID,
			VariantID: cardResult.VariantID,
			OwnerID:   ownerID,
			Quantity:  in.Quantity,
			PriceUSD:  in.PriceUSD,
			PriceCOP:  listing.StandardizedPriceCOP(in.PriceUSD, rate),
			Language:  language,
		})
	default:
		err = findErr
	}
	if err != nil {
		return ManualListingResult{}, fmt.Errorf("publicar el stock de %q: %w", cardResult.CardName, err)
	}

	result := ManualListingResult{
		ListingID:   l.ID,
		CardID:      cardResult.CardID,
		CardName:    cardResult.CardName,
		ExternalID:  cardResult.ExternalID,
		VariantName: cardResult.VariantName,
		ExpansionID: cardResult.ExpansionID,
		Quantity:    l.Quantity,
		PriceUSD:    l.PriceUSD,
		PriceCOP:    l.PriceCOP,
		Status:      l.Status,
		CardCreated: cardResult.CardCreated,
		Merged:      merged,
		ImageURL:    imageSource.URL,
	}

	// La imagen subida se guarda en la base y se sirve desde
	// /card-images/{id}, igual que el catálogo resuelve las de Scrydex. Si en
	// cambio llegó una URL, se enlaza tal cual para que la carta no se quede
	// sin frontal: validarla sin guardarla devolvería una respuesta anunciando
	// una imagen que el catálogo nunca llegaría a mostrar.
	switch {
	case !imageSource.Upload.IsEmpty():
		imageID, err := uc.images.Store(ctx, cardResult.CardID, imageSource.Upload)
		if err != nil {
			return ManualListingResult{}, fmt.Errorf("%w: %v", ErrImageAttachment, err)
		}
		result.ImageURL = fmt.Sprintf("/card-images/%d", imageID)
	case imageSource.URL != "":
		if err := uc.images.Link(ctx, cardResult.CardID, imageSource.URL); err != nil {
			return ManualListingResult{}, fmt.Errorf("%w: %v", ErrImageAttachment, err)
		}
	}

	return result, nil
}

// findDefaultOwner busca el propietario marcado como is_default, que es el que
// recibe el stock cuando el staff no elige otro en el formulario.
func (uc *ManualListingUseCase) findDefaultOwner(ctx context.Context) (owner.Owner, error) {
	owners, err := uc.owners.ListAll(ctx)
	if err != nil {
		return owner.Owner{}, err
	}
	for _, o := range owners {
		if o.IsDefault {
			return o, nil
		}
	}
	return owner.Owner{}, errors.New("no hay propietario por defecto configurado")
}

// ValidateImageURL comprueba que la URL del frontal tenga una forma que el
// catálogo pueda resolver.
func ValidateImageURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("la URL de la imagen no es válida: %w", err)
	}
	// Sin esquema ni host no se puede resolver: sería una ruta relativa
	// publicada sin el dominio del hosting donde vive el archivo.
	if parsed.Scheme == "" || parsed.Host == "" {
		return errors.New("la URL de la imagen debe empezar por http:// o https://")
	}
	switch parsed.Scheme {
	case "http", "https":
		return nil
	}
	return errors.New("la URL de la imagen debe empezar por http:// o https://")
}
