package http

import (
	"net/http"
	"strconv"

	appCatalog "trample-back/internal/application/catalog"
	"trample-back/internal/ports/out"

	"github.com/go-chi/chi/v5"
)

type MagicHandler struct {
	search     *appCatalog.SearchScrydex
	expansions *appCatalog.SyncExpansionsUseCase
	importCard *appCatalog.ImportCardUseCase
}

func NewMagicHandler(search *appCatalog.SearchScrydex, expansions *appCatalog.SyncExpansionsUseCase, importCard *appCatalog.ImportCardUseCase) *MagicHandler {
	return &MagicHandler{search: search, expansions: expansions, importCard: importCard}
}

// Search busca cartas de Magic en Scrydex.
//
//	@Summary      Buscar cartas Magic
//	@Tags         magic
//	@Accept       json
//	@Produce      json
//
// @Param        body  body      object{name=string,expansion_code=string,rarity=string,variants=[]string,type=string}  false  "Filtros (name es requerido)"
// @Success      200   {object}  object{search_id=string,total=integer,cards=[]catalog.Card}
// @Failure      400   {object}  object{error=string}
// @Router       /scrydex/magic/cards [post]
func (h *MagicHandler) Search(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name          string   `json:"name"`
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
		ExpansionCode: body.ExpansionCode,
		Rarity:        body.Rarity,
		Variants:      body.Variants,
		Type:          body.Type,
	})
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]any{
		"search_id": result.SearchID,
		"total":     len(result.Cards),
		"cards":     result.Cards,
	})
}

// POST /scrydex/magic/cards/{id}
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
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	JSON(w, http.StatusOK, card)
}

// POST /admin/magic/expansions/sync
func (h *MagicHandler) SyncExpansions(w http.ResponseWriter, r *http.Request) {
	count, err := h.expansions.SyncMTG(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]int{"synced": count})
}

// GET /catalog/magic/expansions
func (h *MagicHandler) ListExpansions(w http.ResponseWriter, r *http.Request) {
	expansions, err := h.expansions.ListMTG(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, expansions)
}

// DELETE /admin/magic/cards/{id}
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

// PUT /admin/magic/cards/{id}
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
