package http

import (
	"net/http"

	appCatalog "trample-back/internal/application/catalog"
	"trample-back/internal/ports/out"

	"github.com/go-chi/chi/v5"
)

type MagicHandler struct {
	catalog *appCatalog.SearchScrydex
}

func NewMagicHandler(catalog *appCatalog.SearchScrydex) *MagicHandler {
	return &MagicHandler{catalog: catalog}
}

// Search busca cartas de Magic en Scrydex.
//
//	@Summary      Buscar cartas Magic
//	@Tags         magic
//	@Accept       json
//	@Produce      json
//	@Param        body  body      object{name=string,expansion_code=string,rarity=string,variants=[]string}  false  "Filtros"
//	@Success      200   {array}   catalog.Card
//	@Failure      400   {object}  object{error=string}
//	@Router       /scrydex/magic/cards [post]
func (h *MagicHandler) Search(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name          string   `json:"name"`
		ExpansionCode string   `json:"expansion_code"`
		Rarity        string   `json:"rarity"`
		Variants      []string `json:"variants"`
	}
	if err := Decode(r, &body); err != nil {
		Error(w, http.StatusBadRequest, "body JSON inválido")
		return
	}
	cards, err := h.catalog.Search(r.Context(), out.SearchParams{
		GameCode:      "mtg",
		Name:          body.Name,
		ExpansionCode: body.ExpansionCode,
		Rarity:        body.Rarity,
		Variants:      body.Variants,
	})
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	JSON(w, http.StatusOK, cards)
}

// FetchOne obtiene una carta de Magic por ID.
//
//	@Summary      Obtener carta Magic por ID
//	@Tags         magic
//	@Accept       json
//	@Produce      json
//	@Param        id    path      string                     true   "ID de la carta"
//	@Param        body  body      object{variants=[]string}  false  "Variantes"
//	@Success      200   {object}  catalog.Card
//	@Failure      400   {object}  object{error=string}
//	@Router       /scrydex/magic/cards/{id} [post]
func (h *MagicHandler) FetchOne(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Variants []string `json:"variants"`
	}
	if err := Decode(r, &body); err != nil {
		Error(w, http.StatusBadRequest, "body JSON inválido")
		return
	}
	card, err := h.catalog.FetchOne(r.Context(), "mtg", chi.URLParam(r, "id"), body.Variants)
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	JSON(w, http.StatusOK, card)
}
