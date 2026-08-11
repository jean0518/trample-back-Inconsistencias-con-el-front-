package http

import (
	"net/http"

	appCatalog "trample-back/internal/application/catalog"
	"trample-back/internal/ports/out"

	"github.com/go-chi/chi/v5"
)

type PokemonHandler struct {
	search     *appCatalog.SearchScrydex
	expansions *appCatalog.SyncExpansionsUseCase
	importCard *appCatalog.ImportCardUseCase
}

func NewPokemonHandler(
	search *appCatalog.SearchScrydex,
	expansions *appCatalog.SyncExpansionsUseCase,
	importCard *appCatalog.ImportCardUseCase,
) *PokemonHandler {
	return &PokemonHandler{search: search, expansions: expansions, importCard: importCard}
}

// POST /scrydex/pokemon/cards
func (h *PokemonHandler) Search(w http.ResponseWriter, r *http.Request) {
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
	cards, err := h.search.Search(r.Context(), out.SearchParams{
		GameCode:      "pokemon",
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

// POST /scrydex/pokemon/cards/{id}
func (h *PokemonHandler) FetchOne(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Variants []string `json:"variants"`
	}
	if err := Decode(r, &body); err != nil {
		Error(w, http.StatusBadRequest, "body JSON inválido")
		return
	}
	card, err := h.search.FetchOne(r.Context(), "pokemon", chi.URLParam(r, "id"), body.Variants)
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	JSON(w, http.StatusOK, card)
}

// POST /admin/pokemon/expansions/sync
func (h *PokemonHandler) SyncExpansions(w http.ResponseWriter, r *http.Request) {
	count, err := h.expansions.SyncPokemon(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]int{"synced": count})
}

// GET /catalog/pokemon/expansions
func (h *PokemonHandler) ListExpansions(w http.ResponseWriter, r *http.Request) {
	expansions, err := h.expansions.ListPokemon(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, expansions)
}

// POST /admin/pokemon/cards/import
func (h *PokemonHandler) ImportCard(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name          string `json:"name"`
		ExpansionName string `json:"expansion_name"`
		Rarity        string `json:"rarity"`
	}
	if err := Decode(r, &body); err != nil {
		Error(w, http.StatusBadRequest, "body JSON inválido")
		return
	}
	if body.Name == "" {
		Error(w, http.StatusBadRequest, "name es requerido")
		return
	}
	cards, err := h.importCard.ImportPokemon(r.Context(), body.Name, body.ExpansionName, body.Rarity)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]any{
		"imported": len(cards),
		"cards":    cards,
	})
}
