package http

import (
	"net/http"
	"strconv"

	appCatalog "trample-back/internal/application/catalog"
	"trample-back/internal/domain/catalog"
)

type GamesHandler struct {
	games      *appCatalog.GamesUseCase
	expansions *appCatalog.SyncExpansionsUseCase
}

func NewGamesHandler(games *appCatalog.GamesUseCase, expansions *appCatalog.SyncExpansionsUseCase) *GamesHandler {
	return &GamesHandler{games: games, expansions: expansions}
}

type gameResponse struct {
	ID   int64  `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

type expansionResponse struct {
	ID         int64  `json:"ID"`
	GameID     int64  `json:"GameID"`
	ExternalID string `json:"ExternalID"`
	Name       string `json:"Name"`
	Code       string `json:"Code"`
	ReleasedAt string `json:"ReleasedAt,omitempty"`
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

// ListExpansions devuelve las expansiones guardadas localmente, filtradas
// opcionalmente por juego.
//
//	@Summary      Listar expansiones
//	@Tags         catalog
//	@Produce      json
//	@Param        game_id  query     int  false  "ID del juego (opcional)"
//	@Success      200      {array}   expansionResponse
//	@Failure      400      {object}  object{error=string}
//	@Failure      500      {object}  object{error=string}
//	@Router       /expansions [get]
func (h *GamesHandler) ListExpansions(w http.ResponseWriter, r *http.Request) {
	var expansions []catalog.Expansion
	var err error

	if idStr := r.URL.Query().Get("game_id"); idStr != "" {
		gameID, convErr := strconv.ParseInt(idStr, 10, 64)
		if convErr != nil {
			Error(w, http.StatusBadRequest, "game_id inválido")
			return
		}
		expansions, err = h.expansions.ListByGameID(r.Context(), gameID)
	} else {
		expansions, err = h.expansions.ListAll(r.Context())
	}
	if err != nil {
		Error(w, http.StatusInternalServerError, "error interno del servidor")
		return
	}

	resp := make([]expansionResponse, 0, len(expansions))
	for _, e := range expansions {
		resp = append(resp, expansionResponse{
			ID:         e.ID,
			GameID:     e.GameID,
			ExternalID: e.ExternalID,
			Name:       e.Name,
			Code:       e.Code,
			ReleasedAt: e.ReleaseDate,
		})
	}
	JSON(w, http.StatusOK, resp)
}
