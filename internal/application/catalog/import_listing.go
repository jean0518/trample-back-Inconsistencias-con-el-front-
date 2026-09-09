package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"trample-back/internal/domain/catalog"
	"trample-back/internal/domain/listing"
	"trample-back/internal/domain/owner"
	"trample-back/internal/ports/out"
)

// ListingItem son los datos de publicación (inventario) para una carta
// seleccionada. VariantName indica el acabado a publicar (Normal, Foil, etc.);
// si no se envía, se usa la variante por defecto. Si PriceUSD no se envía, se
// usa el precio de mercado NM de la variante elegida; si Language no viene, se
// usa "Inglés".
type ListingItem struct {
	ExternalID  string   `json:"external_id"`
	VariantName string   `json:"variant_name,omitempty"`
	Quantity    int      `json:"quantity"`
	PriceUSD    *float64 `json:"price_usd,omitempty"`
	Language    string   `json:"language,omitempty"`
	OwnerID     *int64   `json:"owner_id,omitempty"`
}

// ImportListingGroup agrupa los items que provienen de una misma búsqueda.
// El back resuelve cada carta desde la caché de esa búsqueda (sin volver a
// consultar Scrydex y sin aceptar datos de carta enviados por el cliente).
type ImportListingGroup struct {
	SearchID string        `json:"search_id"`
	Items    []ListingItem `json:"items"`
}

// ImportedListing resume el listing creado para la respuesta HTTP.
// Merged indica que la carta ya estaba en inventario y solo se sumó stock.
type ImportedListing struct {
	ListingID   int64   `json:"listing_id"`
	GameCode    string  `json:"game_code"`
	ExternalID  string  `json:"external_id"`
	CardName    string  `json:"card_name"`
	VariantName string  `json:"variant_name"`
	Quantity    int     `json:"quantity"`
	PriceUSD    float64 `json:"price_usd"`
	PriceCOP    float64 `json:"price_cop"`
	Status      string  `json:"status"`
	Merged      bool    `json:"merged"`
}

// defaultVariant es la variente usada cuando el admin no especifica un
// acabado: la primera que devuelve Scrydex para la carta (normalmente la
// estándar).
func defaultVariant(card catalog.Card) (catalog.Variant, error) {
	if len(card.Variants) == 0 {
		return catalog.Variant{}, fmt.Errorf("la carta %q no tiene variantes disponibles", card.Name)
	}
	return card.Variants[0], nil
}

// resolveVariant elige la variante de la carta solicitada por nombre
// (insensible a mayúsculas). Si variantName viene vacío, usa la variante por
// defecto.
func resolveVariant(card catalog.Card, variantName string) (catalog.Variant, error) {
	if variantName == "" {
		return defaultVariant(card)
	}
	for _, v := range card.Variants {
		if strings.EqualFold(v.Name, variantName) {
			return v, nil
		}
	}
	names := make([]string, 0, len(card.Variants))
	for _, v := range card.Variants {
		names = append(names, v.Name)
	}
	return catalog.Variant{}, fmt.Errorf(
		"la carta %q no tiene la variante %q (disponibles: %s)",
		card.Name, variantName, strings.Join(names, ", "),
	)
}

// ImportListingUseCase importa cartas seleccionadas al catálogo y crea sus
// listings (inventario) en un solo flujo, con la cantidad indicada por el admin.
type ImportListingUseCase struct {
	search   *SearchScrydex
	cards    out.CardRepository
	listings out.ListingRepository
	owners   out.OwnerRepository
	trm      out.TRMClient
}

func NewImportListingUseCase(
	search *SearchScrydex,
	cards out.CardRepository,
	listings out.ListingRepository,
	owners out.OwnerRepository,
	trm out.TRMClient,
) *ImportListingUseCase {
	return &ImportListingUseCase{search: search, cards: cards, listings: listings, owners: owners, trm: trm}
}

func (uc *ImportListingUseCase) Execute(
	ctx context.Context,
	sellerID int64,
	groups []ImportListingGroup,
) ([]ImportedListing, error) {
	if sellerID <= 0 {
		return nil, fmt.Errorf("seller_id inválido")
	}
	if len(groups) == 0 {
		return nil, fmt.Errorf("groups es requerido")
	}

	rate, err := uc.trm.GetRate(ctx)
	if err != nil {
		return nil, fmt.Errorf("obtener TRM: %w", err)
	}

	// Resolver el owner por defecto (trampleStore).
	defaultOwner, err := uc.findDefaultOwner(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolver propietario por defecto: %w", err)
	}

	var created []ImportedListing
	seen := make(map[string]bool)

	for _, g := range groups {
		if g.SearchID == "" || len(g.Items) == 0 {
			return nil, fmt.Errorf("cada grupo requiere search_id e items")
		}
		cached, ok := uc.search.get(g.SearchID)
		if !ok {
			return nil, fmt.Errorf("la búsqueda %s expiró, repetila e intentá de nuevo", g.SearchID)
		}
		byID := make(map[string]catalog.Card, len(cached.Cards))
		for _, c := range cached.Cards {
			byID[c.ExternalID] = c
		}

		for _, item := range g.Items {
			card, ok := byID[item.ExternalID]
			if !ok {
				return nil, fmt.Errorf("la carta %q no está en los resultados de la búsqueda %s", item.ExternalID, g.SearchID)
			}
			if item.Quantity < 1 {
				return nil, fmt.Errorf("la cantidad debe ser al menos 1 para %q", card.Name)
			}

			variant, err := resolveVariant(card, item.VariantName)
			if err != nil {
				return nil, err
			}
			// Una carta puede publicarse varias veces en el mismo request si
			// el acabado difiere (p. ej. Normal y Foil).
			key := cached.GameCode + ":" + card.ExternalID + ":" + variant.Name
			if seen[key] {
				continue
			}
			seen[key] = true

			priceUSD, priceCOP, err := resolvePrice(item.PriceUSD, variant, rate, card.Name)
			if err != nil {
				return nil, err
			}
			language := item.Language
			if language == "" {
				language = "Inglés"
			}
			ownerID := defaultOwner.ID
			if item.OwnerID != nil && *item.OwnerID > 0 {
				ownerID = *item.OwnerID
			}

			if err := uc.cards.SyncCard(ctx, cached.GameCode, card); err != nil {
				return nil, fmt.Errorf("guardar carta %q: %w", card.Name, err)
			}
			variantID, err := uc.cards.GetVariantID(ctx, cached.GameCode, card.ExternalID, variant.Name)
			if err != nil {
				return nil, fmt.Errorf("resolver variante de %q: %w", card.Name, err)
			}

			// Si el vendedor ya tiene un listing vigente para esta variante
			// con el mismo idioma y propietario, se suma stock. Si no, se
			// crea un listing nuevo (stock separado).
			existing, findErr := uc.listings.FindBySellerAndVariantLanguageOwner(ctx, sellerID, variantID, language, ownerID)
			var l listing.Listing
			merged := false
			switch {
			case findErr == nil:
				l, err = uc.listings.AddQuantity(ctx, listing.UpdateStockInput{
					ID:       existing.ID,
					SellerID: sellerID,
					Quantity: item.Quantity,
				})
				merged = true
			case errors.Is(findErr, listing.ErrNotFound):
				l, err = uc.listings.Create(ctx, listing.CreateInput{
					SellerID:  sellerID,
					VariantID: variantID,
					OwnerID:   ownerID,
					Quantity:  item.Quantity,
					PriceUSD:  priceUSD,
					PriceCOP:  priceCOP,
					Language:  language,
				})
			default:
				err = findErr
			}
			if err != nil {
				return nil, fmt.Errorf("crear listing de %q: %w", card.Name, err)
			}

			created = append(created, ImportedListing{
				ListingID:   l.ID,
				GameCode:    cached.GameCode,
				ExternalID:  card.ExternalID,
				CardName:    card.Name,
				VariantName: variant.Name,
				Quantity:    l.Quantity,
				PriceUSD:    l.PriceUSD,
				PriceCOP:    l.PriceCOP,
				Status:      l.Status,
				Merged:      merged,
			})
		}
	}

	if len(created) == 0 {
		return nil, fmt.Errorf("no se creó ningún listing")
	}
	return created, nil
}

// findDefaultOwner busca el propietario marcado como is_default.
func (uc *ImportListingUseCase) findDefaultOwner(ctx context.Context) (owner.Owner, error) {
	owners, err := uc.owners.ListAll(ctx)
	if err != nil {
		return owner.Owner{}, err
	}
	for _, o := range owners {
		if o.IsDefault {
			return o, nil
		}
	}
	return owner.Owner{}, fmt.Errorf("no hay propietario por defecto configurado")
}

// resolvePrice define el precio del listing: prioriza el precio enviado por el
// admin; si no hay, usa el precio de mercado NM de la variante. El valor en
// COP se calcula con la TRM del día.
func resolvePrice(sent *float64, variant catalog.Variant, rate float64, cardName string) (float64, float64, error) {
	usd := 0.0
	switch {
	case sent != nil && *sent > 0:
		usd = *sent
	case variant.NMPrice != nil && variant.NMPrice.MarketUSD > 0:
		usd = variant.NMPrice.MarketUSD
	default:
		return 0, 0, fmt.Errorf("la carta %q no tiene precio de mercado: indicá price_usd", cardName)
	}
	cop := listing.StandardizedPriceCOP(usd, rate)
	return usd, cop, nil
}
