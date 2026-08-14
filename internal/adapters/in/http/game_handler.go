package http

import (
	"net/http"
	appCatalog "trample-back/internal/application/catalog"
)

type GamesHandler struct {
	games *appCatalog.GamesUseCase
}

func NewGamesHandler(games *appCatalog.GamesUseCase) *GamesHandler {
	return &GamesHandler{games: games}
}

type gameResponse struct {
	ID   int64  `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

// List devuelve los juegos soportados por el catálogo.
//
//	@Summary      Listar juegos
//	@Tags         catalog
//	@Produce      json
//	@Success      200  {array}   gameResponse
//	@Failure      500  {object}  object{error=string}
//	@Router       /games [get]
func (h *GamesHandler) List(w http.ResponseWriter, r *http.Request) {
	games, err := h.games.ListGames(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, "error interno del servidor")
		return
	}

	resp := make([]gameResponse, 0, len(games))
	for _, g := range games {
		resp = append(resp, gameResponse{ID: g.ID, Code: g.Code, Name: g.Name})
	}
	JSON(w, http.StatusOK, resp)
}
