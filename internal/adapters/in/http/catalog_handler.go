package http

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	appCatalog "trample-back/internal/application/catalog"
	"trample-back/internal/ports/out"
)

type CatalogHandler struct {
	listCards *appCatalog.ListCardsUseCase
}

func NewCatalogHandler(listCards *appCatalog.ListCardsUseCase) *CatalogHandler {
	return &CatalogHandler{listCards: listCards}
}

// ListCards devuelve las cartas guardadas en la DB con paginación y filtros opcionales.
//
// @Summary      Listar cartas del catálogo
// @Tags         catalog
// @Produce      json
// @Param        game_code     query  string  false  "Código del juego (pokemon, mtg, riftbound)"
// @Param        expansion_id  query  int     false  "ID de la expansión"
// @Param        owner_id      query  int     false  "ID del propietario"
// @Param        language      query  string  false  "Idioma de la carta"
// @Param        name          query  string  false  "Nombre parcial de la carta"
// @Param        rarity        query  string  false  "Rareza exacta (ej: Rare Holo)"
// @Param        max_price     query  int     false  "Precio máximo en COP (por la variante más barata)"
// @Param        sort          query  string  false  "Orden: relevance | price-asc | price-desc | name"
// @Param        page          query  int     false  "Página (default 1)"
// @Param        page_size     query  int     false  "Resultados por página (default 20, max 100)"
// @Success      200  {object}  object{page=int,page_size=int,total=int,cards=[]catalog.CardSummary}
// @Failure      500  {object}  object{error=string}
// @Router       /catalog/cards [get]
func (h *CatalogHandler) ListCards(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	expansionID, _ := strconv.ParseInt(q.Get("expansion_id"), 10, 64)
	ownerID, _ := strconv.ParseInt(q.Get("owner_id"), 10, 64)
	maxPrice, _ := strconv.ParseInt(q.Get("max_price"), 10, 64)
	page, _ := strconv.Atoi(q.Get("page"))
	pageSize, _ := strconv.Atoi(q.Get("page_size"))

	result, err := h.listCards.List(r.Context(), out.ListCardsParams{
		GameCode:    q.Get("game_code"),
		ExpansionID: expansionID,
		OwnerID:     ownerID,
		Language:    q.Get("language"),
		Name:        q.Get("name"),
		Rarity:      q.Get("rarity"),
		MaxPrice:    maxPrice,
		Sort:        q.Get("sort"),
		Page:        page,
		PageSize:    pageSize,
	})
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	JSON(w, http.StatusOK, map[string]any{
		"page":      result.Page,
		"page_size": result.PageSize,
		"total":     result.Total,
		"cards":     result.Cards,
	})
}

// GetCard devuelve una carta del catálogo por su ID.
//
// @Summary      Obtener una carta del catálogo
// @Tags         catalog
// @Produce      json
// @Param        id  path  int  true  "ID de la carta"
// @Success      200  {object}  catalog.CardSummary
// @Failure      400  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Failure      500  {object}  object{error=string}
// @Router       /catalog/cards/{id} [get]
func (h *CatalogHandler) GetCard(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		Error(w, http.StatusBadRequest, "id de carta inválido")
		return
	}

	result, err := h.listCards.List(r.Context(), out.ListCardsParams{
		CardID:   id,
		Page:     1,
		PageSize: 1,
	})
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(result.Cards) == 0 {
		Error(w, http.StatusNotFound, "carta no encontrada")
		return
	}

	JSON(w, http.StatusOK, result.Cards[0])
}
