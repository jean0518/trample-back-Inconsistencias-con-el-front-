package http

import (
	"net/http"
	"strconv"

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

// Search busca cartas de Pokémon en Scrydex.
//
//	@Summary      Buscar cartas Pokémon
//	@Tags         pokemon
//	@Accept       json
//	@Produce      json
//	@Param        body  body      object{name=string,expansion_code=string,rarity=string,variants=[]string,type=string}  false  "Filtros de búsqueda (name es requerido)"
//	@Success      200   {object}  object{search_id=string,total=integer,cards=[]catalog.Card}
//	@Failure      400   {object}  object{error=string}
//	@Router       /scrydex/pokemon/cards [post]
func (h *PokemonHandler) Search(w http.ResponseWriter, r *http.Request) {
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
		GameCode:      "pokemon",
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

// FetchOne obtiene una carta de Pokémon por ID.
//
//	@Summary      Obtener carta Pokémon por ID
//	@Tags         pokemon
//	@Accept       json
//	@Produce      json
//	@Param        id    path      string                       true   "ID de la carta"
//	@Param        body  body      object{variants=[]string}    false  "Variantes"
//	@Success      200   {object}  catalog.Card
//	@Failure      400   {object}  object{error=string}
//	@Router       /scrydex/pokemon/cards/{id} [post]
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

// DeleteCard elimina una carta de Pokémon de la DB.
func (h *PokemonHandler) DeleteCard(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		Error(w, http.StatusBadRequest, "id inválido")
		return
	}
	if err := h.importCard.DeletePokemon(r.Context(), id); err != nil {
		Error(w, http.StatusNotFound, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]string{"deleted": chi.URLParam(r, "id")})
}

// RefreshCard re-sincroniza una carta desde Scrydex (precios e imágenes frescos).
func (h *PokemonHandler) RefreshCard(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		Error(w, http.StatusBadRequest, "id inválido")
		return
	}
	card, err := h.importCard.RefreshPokemon(r.Context(), id)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, card)
}

// SyncExpansions sincroniza las expansiones de Pokémon desde Scrydex.
//
//	@Summary      Sincronizar expansiones Pokémon
//	@Tags         admin
//	@Produce      json
//	@Success      200  {object}  object{synced=integer}
//	@Failure      500  {object}  object{error=string}
//	@Router       /admin/pokemon/expansions/sync [post]
func (h *PokemonHandler) SyncExpansions(w http.ResponseWriter, r *http.Request) {
	count, err := h.expansions.SyncPokemon(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]int{"synced": count})
}

// ListExpansions lista las expansiones de Pokémon guardadas localmente.
//
//	@Summary      Listar expansiones Pokémon
//	@Tags         pokemon
//	@Produce      json
//	@Success      200  {array}   catalog.Expansion
//	@Failure      500  {object}  object{error=string}
//	@Router       /catalog/pokemon/expansions [get]
func (h *PokemonHandler) ListExpansions(w http.ResponseWriter, r *http.Request) {
	expansions, err := h.expansions.ListPokemon(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, expansions)
}

// ImportCard importa cartas de Pokémon al catálogo local.
//
//	@Summary      Importar cartas Pokémon
//	@Tags         admin
//	@Accept       json
//	@Produce      json
//	@Param        body  body      object{name=string,expansion_name=string,rarity=string}  true  "Datos de la carta"
//	@Success      200   {object}  object{imported=integer,cards=[]catalog.Card}
//	@Failure      400   {object}  object{error=string}
//	@Failure      500   {object}  object{error=string}
//	@Router       /admin/pokemon/cards/import [post]
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

// ImportCards importa las cartas seleccionadas de una búsqueda previa.
//
//	@Summary      Importar cartas seleccionadas de una búsqueda
//	@Tags         admin
//	@Accept       json
//	@Produce      json
//	@Param        body  body      object{search_id=string,external_ids=[]string}  true  "search_id devuelto por la búsqueda + IDs de las cartas elegidas"
//	@Success      200   {object}  object{imported=integer,cards=[]catalog.Card}
//	@Failure      400   {object}  object{error=string}
//	@Failure      500   {object}  object{error=string}
//	@Router       /admin/cards/import [post]
func (h *PokemonHandler) ImportCards(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SearchID    string   `json:"search_id"`
		ExternalIDs []string `json:"external_ids"`
	}
	if err := Decode(r, &body); err != nil {
		Error(w, http.StatusBadRequest, "body JSON inválido")
		return
	}
	cards, err := h.importCard.ImportBySearch(r.Context(), body.SearchID, body.ExternalIDs)
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]any{
		"imported": len(cards),
		"cards":    cards,
	})
}
