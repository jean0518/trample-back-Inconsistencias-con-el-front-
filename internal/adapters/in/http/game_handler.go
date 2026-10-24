package http

import (
	"errors"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	appCatalog "trample-back/internal/application/catalog"
	"trample-back/internal/domain/catalog"
)

type GamesHandler struct {
	games      *appCatalog.GamesUseCase
	expansions *appCatalog.SyncExpansionsUseCase
	deleteGame *appCatalog.DeleteGameUseCase
}

func NewGamesHandler(games *appCatalog.GamesUseCase, expansions *appCatalog.SyncExpansionsUseCase, deleteGame *appCatalog.DeleteGameUseCase) *GamesHandler {
	return &GamesHandler{games: games, expansions: expansions, deleteGame: deleteGame}
}

type gameResponse struct {
	ID                int64  `json:"id"`
	Code              string `json:"code"`
	Name              string `json:"name"`
	ShortName         string `json:"short_name,omitempty"`
	Accent            string `json:"accent,omitempty"`
	Tagline           string `json:"tagline,omitempty"`
	Icon              string `json:"icon,omitempty"`
	Logo              string `json:"logo,omitempty"`
	IconURL           string `json:"icon_url,omitempty"`
	LogoURL           string `json:"logo_url,omitempty"`
	HasCards          bool   `json:"has_cards"`
	Active            bool   `json:"active"`
	ProductsCount     int64  `json:"products_count"`
	CardsCount        int64  `json:"cards_count"`
	CatalogCardsCount int64  `json:"catalog_cards_count"`
	ExpansionsCount   int64  `json:"expansions_count"`
}

type expansionResponse struct {
	ID         int64  `json:"ID"`
	GameID     int64  `json:"GameID"`
	ExternalID string `json:"ExternalID"`
	Name       string `json:"Name"`
	Code       string `json:"Code"`
	ReleasedAt string `json:"ReleasedAt,omitempty"`
	LogoURL    string `json:"LogoURL,omitempty"`
	SymbolURL  string `json:"SymbolURL,omitempty"`
}

func toGameResponse(g catalog.Game) gameResponse {
	icon := g.IconURL
	logo := g.LogoURL
	return gameResponse{
		ID: g.ID, Code: g.Code, Name: g.Name,
		ShortName: g.ShortName, Accent: g.Accent, Tagline: g.Tagline,
		Icon: icon, Logo: logo, IconURL: icon, LogoURL: logo,
		HasCards: g.HasCards, Active: g.Active,
		ProductsCount: g.ProductsCount, CardsCount: g.CardsCount,
		CatalogCardsCount: g.CatalogCardsCount, ExpansionsCount: g.ExpansionsCount,
	}
}

func (h *GamesHandler) List(w http.ResponseWriter, r *http.Request) {
	games, err := h.games.ListGames(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, "error interno del servidor")
		return
	}
	resp := make([]gameResponse, 0, len(games))
	for _, g := range games {
		resp = append(resp, toGameResponse(g))
	}
	JSON(w, http.StatusOK, resp)
}

func (h *GamesHandler) ListAdmin(w http.ResponseWriter, r *http.Request) {
	games, err := h.games.ListGamesAdmin(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, "error interno del servidor")
		return
	}
	resp := make([]gameResponse, 0, len(games))
	for _, g := range games {
		resp = append(resp, toGameResponse(g))
	}
	JSON(w, http.StatusOK, resp)
}

func getFile(r *http.Request, names ...string) (*multipart.FileHeader, error) {
	for _, n := range names {
		if _, fh, err := r.FormFile(n); err == nil {
			return fh, nil
		}
	}
	return nil, http.ErrMissingFile
}

func (h *GamesHandler) Create(w http.ResponseWriter, r *http.Request) {
	ct := r.Header.Get("Content-Type")
	if strings.Contains(ct, "multipart/form-data") {
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			Error(w, http.StatusBadRequest, "formulario inválido")
			return
		}
		in := catalog.GameInput{
			Code:      r.FormValue("code"),
			Name:      r.FormValue("name"),
			ShortName: r.FormValue("short_name"),
			Accent:    r.FormValue("accent"),
			Tagline:   r.FormValue("tagline"),
			IconURL:   r.FormValue("icon_url"),
			LogoURL:   r.FormValue("logo_url"),
		}
		if v := r.FormValue("has_cards"); v != "" {
			b := v == "true" || v == "1"
			in.HasCards = &b
		}
		g, err := h.games.CreateGame(r.Context(), in)
		if err != nil {
			Error(w, http.StatusBadRequest, err.Error())
			return
		}
		if fh, err := getFile(r, "icon", "file", "image"); err == nil {
			if g2, uerr := h.games.UploadGameIcon(r.Context(), g.ID, fh); uerr == nil {
				g = g2
			}
		}
		if fh, err := getFile(r, "logo", "file", "image"); err == nil {
			if g2, uerr := h.games.UploadGameLogo(r.Context(), g.ID, fh); uerr == nil {
				g = g2
			}
		}
		JSON(w, http.StatusCreated, toGameResponse(g))
		return
	}

	var req struct {
		Code      string `json:"code"`
		Name      string `json:"name"`
		ShortName string `json:"short_name"`
		Accent    string `json:"accent"`
		Tagline   string `json:"tagline"`
		IconURL   string `json:"icon_url"`
		LogoURL   string `json:"logo_url"`
		HasCards  *bool  `json:"has_cards"`
	}
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	g, err := h.games.CreateGame(r.Context(), catalog.GameInput{
		Code: req.Code, Name: req.Name, ShortName: req.ShortName,
		Accent: req.Accent, Tagline: req.Tagline, IconURL: req.IconURL, LogoURL: req.LogoURL, HasCards: req.HasCards,
	})
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	JSON(w, http.StatusCreated, toGameResponse(g))
}

func (h *GamesHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r)
	if !ok {
		Error(w, http.StatusBadRequest, "id inválido")
		return
	}
	var req struct {
		Name      string `json:"name"`
		ShortName string `json:"short_name"`
		Accent    string `json:"accent"`
		Tagline   string `json:"tagline"`
		IconURL   string `json:"icon_url"`
		LogoURL   string `json:"logo_url"`
		HasCards  *bool  `json:"has_cards"`
		Active    *bool  `json:"active"`
	}
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	g, err := h.games.UpdateGame(r.Context(), id, catalog.GameInput{
		Name: req.Name, ShortName: req.ShortName, Accent: req.Accent,
		Tagline: req.Tagline, IconURL: req.IconURL, LogoURL: req.LogoURL, HasCards: req.HasCards, Active: req.Active,
	})
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	JSON(w, http.StatusOK, toGameResponse(g))
}

// Delete elimina un juego y todos sus productos sellados. Solo un
// administrador directo puede hacerlo y debe reconfirmar correo y contraseña.
// Si el juego tiene cartas o expansiones se rechaza con 409.
func (h *GamesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r)
	if !ok {
		Error(w, http.StatusBadRequest, "id inválido")
		return
	}
	authUser, ok := AuthFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "no autenticado")
		return
	}
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := Decode(r, &body); err != nil {
		Error(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	deletedProducts, err := h.deleteGame.Execute(r.Context(), authUser.ID, id, body.Email, body.Password)
	switch {
	case errors.Is(err, appCatalog.ErrDirectAdminRequired):
		Error(w, http.StatusForbidden, err.Error())
		return
	case errors.Is(err, appCatalog.ErrDeleteConfirmation):
		// 422 y no 401: un error de confirmación no debe cerrar la sesión.
		Error(w, http.StatusUnprocessableEntity, err.Error())
		return
	case errors.Is(err, catalog.ErrGameNotFound):
		Error(w, http.StatusNotFound, err.Error())
		return
	case errors.Is(err, catalog.ErrGameHasCards):
		Error(w, http.StatusConflict, err.Error())
		return
	case err != nil:
		Error(w, http.StatusInternalServerError, "error interno del servidor")
		return
	}
	JSON(w, http.StatusOK, map[string]any{"deleted_id": id, "deleted_products": deletedProducts})
}

func (h *GamesHandler) UploadIcon(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r)
	if !ok {
		Error(w, http.StatusBadRequest, "id inválido")
		return
	}
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		Error(w, http.StatusBadRequest, "formulario inválido")
		return
	}
	fh, err := getFile(r, "icon", "file", "image")
	if err != nil {
		Error(w, http.StatusBadRequest, "archivo requerido")
		return
	}
	g, err := h.games.UploadGameIcon(r.Context(), id, fh)
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	JSON(w, http.StatusOK, toGameResponse(g))
}

func (h *GamesHandler) UploadLogo(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r)
	if !ok {
		Error(w, http.StatusBadRequest, "id inválido")
		return
	}
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		Error(w, http.StatusBadRequest, "formulario inválido")
		return
	}
	fh, err := getFile(r, "logo", "file", "image")
	if err != nil {
		Error(w, http.StatusBadRequest, "archivo requerido")
		return
	}
	g, err := h.games.UploadGameLogo(r.Context(), id, fh)
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	JSON(w, http.StatusOK, toGameResponse(g))
}

func parseID(r *http.Request) (int64, bool) {
	idStr := chi.URLParam(r, "id")
	if idStr == "" {
		idStr = r.URL.Query().Get("id")
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	return id, err == nil && id > 0
}

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
			LogoURL:    e.Logo,
			SymbolURL:  e.Symbol,
		})
	}
	JSON(w, http.StatusOK, resp)
}
