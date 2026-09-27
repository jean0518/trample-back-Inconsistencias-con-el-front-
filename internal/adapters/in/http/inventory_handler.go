package http

import (
	"errors"
	"net/http"

	appCatalog "trample-back/internal/application/catalog"
	"trample-back/internal/domain/catalog"
)

// InventoryHandler expone el alta de inventario que hace el staff desde el
// panel: el alta de una carta a mano y la creación de una expansión nueva.
//
// No expone listados ni precios: el alta manual solo necesita estas dos
// operaciones, y el resto del inventario se lee por el catálogo.
type InventoryHandler struct {
	manualListing *appCatalog.ManualListingUseCase
	newExpansion  *appCatalog.NewExpansionUseCase
}

// NewInventoryHandler construye el handler.
func NewInventoryHandler(manualListing *appCatalog.ManualListingUseCase, newExpansion *appCatalog.NewExpansionUseCase) *InventoryHandler {
	return &InventoryHandler{manualListing: manualListing, newExpansion: newExpansion}
}

// manualExpansionResponse es la expansión recién creada con las mismas claves
// PascalCase que devuelve GET /expansions, para que el frontend la añada a su
// desplegable sin traducir el objeto ni volver a pedir la lista.
type manualExpansionResponse struct {
	ID         int64  `json:"ID"`
	GameID     int64  `json:"GameID"`
	ExternalID string `json:"ExternalID"`
	Name       string `json:"Name"`
	Code       string `json:"Code"`
	ReleasedAt string `json:"ReleasedAt,omitempty"`
	LogoURL    string `json:"LogoURL,omitempty"`
	SymbolURL  string `json:"SymbolURL,omitempty"`
}

// CreateExpansion es la acción del botón "+" del selector de expansión del
// formulario de alta.
//
// Responde 201 si la expansión no existía y 200 si ya existía: la operación es
// idempotente porque el identificador se deriva del nombre, y distinguir ambos
// casos deja que el frontend decida si resalta la fila nueva. El 200 no es un
// fallo, es la señal de que el set ya estaba.
//
//	@Summary      Crear expansión (botón "+" del alta manual)
//	@Tags         catalog
//	@Accept       json
//	@Produce      json
//	@Success      201  {object}  manualExpansionResponse
//	@Success      200  {object}  manualExpansionResponse
//	@Failure      400  {object}  object{error=string}
//	@Router       /admin/expansions [post]
func (h *InventoryHandler) CreateExpansion(w http.ResponseWriter, r *http.Request) {
	var body struct {
		GameCode    string `json:"game_code"`
		Name        string `json:"name"`
		Code        string `json:"code"`
		Series      string `json:"series"`
		ReleaseDate string `json:"release_date"`
		Total       int    `json:"total"`
	}
	if err := Decode(r, &body); err != nil {
		Error(w, http.StatusBadRequest, "el cuerpo de la petición no es JSON válido")
		return
	}

	result, err := h.newExpansion.Execute(r.Context(), catalog.NewExpansion{
		GameCode:    body.GameCode,
		Name:        body.Name,
		Code:        body.Code,
		Series:      body.Series,
		ReleaseDate: body.ReleaseDate,
		Total:       body.Total,
	})
	if err != nil {
		// Los errores de validación del dominio ya son texto pensado para el
		// staff ("el nombre de la expansión es requerido"), así que se
		// muestran tal cual en vez de reemplazarlos por un "error interno".
		Error(w, http.StatusBadRequest, err.Error())
		return
	}

	e := result.Expansion
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	JSON(w, status, manualExpansionResponse{
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

// ManualListing da de alta una carta en el inventario.
//
// Acepta JSON o multipart/form-data (ver DecodeManualListing). Cuando se sube
// un archivo, la carta se persiste primero y la imagen después; si la imagen
// falla se responde 422 dejando claro que la carta sí quedó creada, porque
// reenviar el formulario es seguro (el alta es idempotente y el stock se suma
// al listing existente).
//
//	@Summary      Dar de alta una carta a mano en el inventario
//	@Tags         catalog
//	@Accept       json
//	@Accept       mpfd
//	@Produce      json
//	@Success      201  {object}  appCatalog.ManualListingResult
//	@Failure      400  {object}  object{error=string}
//	@Failure      422  {object}  object{error=string}
//	@Router       /admin/cards/manual-listing [post]
func (h *InventoryHandler) ManualListing(w http.ResponseWriter, r *http.Request) {
	form, err := DecodeManualListing(w, r)
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}

	// El seller es el usuario autenticado, nunca algo que venga en el cuerpo:
	// si el staff pudiera elegirlo, podría publicar stock a nombre de otro.
	user, ok := AuthFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "no autenticado")
		return
	}

	result, err := h.manualListing.Execute(r.Context(), user.ID, appCatalog.ManualListingInput{
		GameCode:      form.GameCode,
		ExpansionID:   form.ExpansionID,
		ExpansionName: form.ExpansionName,
		ExternalID:    form.ExternalID,
		Name:          form.Name,
		Number:        form.Number,
		Rarity:        form.Rarity,
		VariantName:   form.VariantName,
		Quantity:      form.Quantity,
		PriceUSD:      form.PriceUSD,
		Language:      form.Language,
		OwnerID:       form.OwnerID,
		ImageURL:      form.ImageURL,
		ImageFile:     form.ImageFile,
	})
	if err != nil {
		// 422 distingue "la carta se guardó pero falló la imagen" de un 400 de
		// validación, para que el frontend sepa que reenviar los mismos datos
		// no lo resuelve sin tener que parsear el mensaje.
		status := http.StatusBadRequest
		if errors.Is(err, appCatalog.ErrImageAttachment) {
			status = http.StatusUnprocessableEntity
		}
		Error(w, status, err.Error())
		return
	}

	JSON(w, http.StatusCreated, result)
}
