package http

import (
	"net/http"
	"strconv"

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
//	@Summary      Listar cartas del catálogo
//	@Tags         catalog
//	@Produce      json
//	@Param        game_code     query  string  false  "Código del juego (pokemon, mtg, riftbound)"
//	@Param        expansion_id  query  int     false  "ID de la expansión"
//	@Param        name          query  string  false  "Nombre parcial de la carta"
//	@Param        rarity        query  string  false  "Rareza exacta (ej: Rare Holo)"
//	@Param        page          query  int     false  "Página (default 1)"
//	@Param        page_size     query  int     false  "Resultados por página (default 20, max 100)"
//	@Success      200  {object}  object{page=int,page_size=int,total=int,cards=[]catalog.CardSummary}
//	@Failure      500  {object}  object{error=string}
//	@Router       /catalog/cards [get]
func (h *CatalogHandler) ListCards(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	expansionID, _ := strconv.ParseInt(q.Get("expansion_id"), 10, 64)
	page, _ := strconv.Atoi(q.Get("page"))
	pageSize, _ := strconv.Atoi(q.Get("page_size"))

	result, err := h.listCards.List(r.Context(), out.ListCardsParams{
		GameCode:    q.Get("game_code"),
		ExpansionID: expansionID,
		Name:        q.Get("name"),
		Rarity:      q.Get("rarity"),
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
