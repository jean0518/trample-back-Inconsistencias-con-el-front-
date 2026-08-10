package http

import (
	"net/http"

	appCatalog "trample-back/internal/application/catalog"
	"trample-back/internal/ports/out"

	"github.com/go-chi/chi/v5"
)

type CardHandler struct {
	catalog *appCatalog.SearchScrydex
}

func NewCardHandler(catalog *appCatalog.SearchScrydex) *CardHandler {
	return &CardHandler{catalog: catalog}
}

// POST /scrydex/cards  — busca por nombre, expansión, rareza y variante
func (h *CardHandler) SearchScrydex(w http.ResponseWriter, r *http.Request) {
	var body out.SearchParams
	if err := Decode(r, &body); err != nil {
		Error(w, http.StatusBadRequest, "body JSON inválido")
		return
	}
	cards, err := h.catalog.Search(r.Context(), body)
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	JSON(w, http.StatusOK, cards)
}

// POST /scrydex/cards/{id}  — trae una carta específica por external_id
func (h *CardHandler) FetchScrydexCard(w http.ResponseWriter, r *http.Request) {
	var body struct {
		GameCode string   `json:"game_code"`
		Variants []string `json:"variants"`
	}
	if err := Decode(r, &body); err != nil {
		Error(w, http.StatusBadRequest, "body JSON inválido")
		return
	}
	card, err := h.catalog.FetchOne(r.Context(), body.GameCode, chi.URLParam(r, "id"), body.Variants)
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	JSON(w, http.StatusOK, card)
}
