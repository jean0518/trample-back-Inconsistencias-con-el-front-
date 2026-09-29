package http

import (
	"net/http"
	"strconv"

	appCatalog "trample-back/internal/application/catalog"
	"trample-back/internal/ports/out"

	"github.com/go-chi/chi/v5"
)

type MagicHandler struct {
	search          *appCatalog.SearchScrydex
	priceByLanguage *appCatalog.PriceByLanguageUseCase
	expansions      *appCatalog.SyncExpansionsUseCase
	importCard      *appCatalog.ImportCardUseCase
	importListing   *appCatalog.ImportListingUseCase
}

func NewMagicHandler(
	search *appCatalog.SearchScrydex,
	priceByLanguage *appCatalog.PriceByLanguageUseCase,
	expansions *appCatalog.SyncExpansionsUseCase,
	importCard *appCatalog.ImportCardUseCase,
	importListing *appCatalog.ImportListingUseCase,
) *MagicHandler {
	return &MagicHandler{
		search:          search,
		priceByLanguage: priceByLanguage,
		expansions:      expansions,
		importCard:      importCard,
		importListing:   importListing,
	}
}

// Search busca cartas de Magic en Scrydex.
//
//	@Summary      Buscar cartas Magic
//	@Tags         magic
//	@Accept       json
//	@Produce      json
//
// @Param        body  body      object{name=string,external_id=string,expansion_code=string,rarity=string,variants=[]string,type=string}  false  "Filtros (name o external_id es requerido)"
// @Success      200   {object}  object{search_id=string,total=integer,cards=[]catalog.Card}
// @Failure      400   {object}  object{error=string}
// @Router       /scrydex/magic/cards [post]
func (h *MagicHandler) Search(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name          string   `json:"name"`
		ExternalID    string   `json:"external_id"`
		ExpansionCode string   `json:"expansion_code"`
		Rarity        string   `json:"rarity"`
		Variants      []string `json:"variants"`
		Type          string   `json:"type"`
	}
	if err := Decode(r, &body); err != nil {
		Error(w, http.StatusBadRequest, "body JSON inválido")
		return
	}
	result, err := h.search.Search(r.Context(), out.SearchParams{
		GameCode:      "mtg",
		Name:          body.Name,
		ExternalID:    body.ExternalID,
		ExpansionCode: body.ExpansionCode,
		Rarity:        body.Rarity,
		Variants:      body.Variants,
		Type:          body.Type,
	})
	if err != nil {
		Error(w, http.StatusBadRequest, friendlyErr(err))
		return
	}
	JSON(w, http.StatusOK, map[string]any{
		"search_id": result.SearchID,
		"total":     len(result.Cards),
		"cards":     result.Cards,
	})
}

// PriceByLanguage devuelve el precio de mercado de una carta de Magic en un
// idioma concreto. El front lo usa al editar un listing para re-preciarlo.
//
//	@Summary      Precio de carta Magic por idioma
//	@Tags         magic
//	@Accept       json
//	@Produce      json
//	@Param        body  body  object{name=string,external_id=string,language=string,variants=[]string}  true  "Filtros con idioma"
//	@Success      200   {object}  catalog.Card
//	@Failure      400   {object}  object{error=string}
//	@Router       /scrydex/magic/cards/price-by-language [post]
func (h *MagicHandler) PriceByLanguage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name       string   `json:"name"`
		ExternalID string   `json:"external_id"`
		Language   string   `json:"language"`
		Variants   []string `json:"variants"`
	}
	if err := Decode(r, &body); err != nil {
		Error(w, http.StatusBadRequest, "body JSON inválido")
		return
	}
	if body.Language == "" {
		Error(w, http.StatusBadRequest, "language es requerido")
		return
	}

	card, err := h.priceByLanguage.Execute(r.Context(), appCatalog.PriceByLanguageInput{
		GameCode:   "mtg",
		ExternalID: body.ExternalID,
		Name:       body.Name,
		Language:   body.Language,
		Variants:   body.Variants,
	})
	if err != nil {
		Error(w, http.StatusBadRequest, friendlyErr(err))
		return
	}
	JSON(w, http.StatusOK, card)
}

func (h *MagicHandler) FetchOne(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Variants []string `json:"variants"`
	}
	if err := Decode(r, &body); err != nil {
		Error(w, http.StatusBadRequest, "body JSON inválido")
		return
	}
	card, err := h.search.FetchOne(r.Context(), "mtg", chi.URLParam(r, "id"), body.Variants)
	if err != nil {
		Error(w, http.StatusBadRequest, friendlyErr(err))
		return
	}
	JSON(w, http.StatusOK, card)
}

func (h *MagicHandler) SyncExpansions(w http.ResponseWriter, r *http.Request) {
	count, err := h.expansions.SyncMTG(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]int{"synced": count})
}

func (h *MagicHandler) ListExpansions(w http.ResponseWriter, r *http.Request) {
	expansions, err := h.expansions.ListMTG(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, expansions)
}

func (h *MagicHandler) DeleteCard(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		Error(w, http.StatusBadRequest, "id inválido")
		return
	}
	if err := h.importCard.DeleteMTG(r.Context(), id); err != nil {
		Error(w, http.StatusNotFound, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]string{"deleted": chi.URLParam(r, "id")})
}

func (h *MagicHandler) ImportToListing(w http.ResponseWriter, r *http.Request) {
	user, ok := AuthFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var body struct {
		Groups []appCatalog.ImportListingGroup `json:"groups"`
	}
	if err := Decode(r, &body); err != nil {
		Error(w, http.StatusBadRequest, "body JSON inválido")
		return
	}

	listings, err := h.importListing.Execute(r.Context(), user.ID, body.Groups)
	if err != nil {
		Error(w, http.StatusBadRequest, friendlyErr(err))
		return
	}
	JSON(w, http.StatusOK, map[string]any{
		"imported": len(listings),
		"listings": listings,
	})
}

func (h *MagicHandler) RefreshCard(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		Error(w, http.StatusBadRequest, "id inválido")
		return
	}
	card, err := h.importCard.RefreshMTG(r.Context(), id)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, card)
}
