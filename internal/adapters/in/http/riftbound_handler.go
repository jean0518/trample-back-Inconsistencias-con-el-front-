package http

import (
	"net/http"

	appCatalog "trample-back/internal/application/catalog"
	"trample-back/internal/ports/out"

	"github.com/go-chi/chi/v5"
)

type RiftboundHandler struct {
	catalog    *appCatalog.SearchScrydex
	expansions *appCatalog.SyncExpansionsUseCase
}

func NewRiftboundHandler(catalog *appCatalog.SearchScrydex, expansions *appCatalog.SyncExpansionsUseCase) *RiftboundHandler {
	return &RiftboundHandler{catalog: catalog, expansions: expansions}
}

// SyncExpansions sincroniza las expansiones de Riftbound desde Scrydex.
//
//	@Summary      Sincronizar expansiones Riftbound
//	@Tags         admin
//	@Produce      json
//	@Success      200  {object}  object{synced=integer}
//	@Failure      500  {object}  object{error=string}
//	@Router       /admin/riftbound/expansions/sync [post]
func (h *RiftboundHandler) SyncExpansions(w http.ResponseWriter, r *http.Request) {
	count, err := h.expansions.SyncRiftbound(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]int{"synced": count})
}

// ListExpansions lista las expansiones de Riftbound guardadas localmente.
//
//	@Summary      Listar expansiones Riftbound
//	@Tags         riftbound
//	@Produce      json
//	@Success      200  {array}   catalog.Expansion
//	@Failure      500  {object}  object{error=string}
//	@Router       /catalog/riftbound/expansions [get]
func (h *RiftboundHandler) ListExpansions(w http.ResponseWriter, r *http.Request) {
	expansions, err := h.expansions.ListRiftbound(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, expansions)
}

// Search busca cartas de Riftbound en Scrydex.
//
//	@Summary      Buscar cartas Riftbound
//	@Tags         riftbound
//	@Accept       json
//	@Produce      json
//
// @Param        body  body      object{name=string,expansion_code=string,rarity=string,variants=[]string,type=string}  false  "Filtros (name es requerido)"
// @Success      200   {object}  object{search_id=string,total=integer,cards=[]catalog.Card}
// @Failure      400   {object}  object{error=string}
// @Router       /scrydex/riftbound/cards [post]
func (h *RiftboundHandler) Search(w http.ResponseWriter, r *http.Request) {
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
	result, err := h.catalog.Search(r.Context(), out.SearchParams{
		GameCode:      "riftbound",
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

// FetchOne obtiene una carta de Riftbound por ID.
//
//	@Summary      Obtener carta Riftbound por ID
//	@Tags         riftbound
//	@Accept       json
//	@Produce      json
//	@Param        id    path      string                     true   "ID de la carta"
//	@Param        body  body      object{variants=[]string}  false  "Variantes"
//	@Success      200   {object}  catalog.Card
//	@Failure      400   {object}  object{error=string}
//	@Router       /scrydex/riftbound/cards/{id} [post]
func (h *RiftboundHandler) FetchOne(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Variants []string `json:"variants"`
	}
	if err := Decode(r, &body); err != nil {
		Error(w, http.StatusBadRequest, "body JSON inválido")
		return
	}
	card, err := h.catalog.FetchOne(r.Context(), "riftbound", chi.URLParam(r, "id"), body.Variants)
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	JSON(w, http.StatusOK, card)
}
