package http

import (
	"errors"
	"net/http"
	"strconv"

	appListing "trample-back/internal/application/listing"
	"trample-back/internal/domain/listing"
)

type ListingHandler struct {
	create *appListing.CreateListingUseCase
	list   *appListing.ListListingsUseCase
	update *appListing.UpdateStockUseCase
	delete *appListing.DeleteListingUseCase
}

func NewListingHandler(
	create *appListing.CreateListingUseCase,
	list *appListing.ListListingsUseCase,
	update *appListing.UpdateStockUseCase,
	delete *appListing.DeleteListingUseCase,
) *ListingHandler {
	return &ListingHandler{create: create, list: list, update: update, delete: delete}
}

type listingResponse struct {
	ID            int64   `json:"ID"`
	SellerID      int64   `json:"SellerID"`
	VariantID     int64   `json:"VariantID"`
	GameName      string  `json:"GameName"`
	CardName      string  `json:"CardName"`
	CardImage     string  `json:"CardImage"`
	ExpansionName string  `json:"ExpansionName"`
	VariantName   string  `json:"VariantName"`
	Quantity      int     `json:"Quantity"`
	PriceUSD      float64 `json:"PriceUSD"`
	PriceCOP      float64 `json:"PriceCOP"`
	Status        string  `json:"Status"`
	Language      string  `json:"Language"`
	CreatedAt     string  `json:"CreatedAt"`
	UpdatedAt     string  `json:"UpdatedAt"`
}

type createListingRequest struct {
	VariantID int64   `json:"variant_id"`
	Quantity  int     `json:"quantity"`
	PriceUSD  float64 `json:"price_usd"`
	Language  string  `json:"language"`
}

func newListingResponse(l listing.Listing) listingResponse {
	return listingResponse{
		ID:            l.ID,
		SellerID:      l.SellerID,
		VariantID:     l.VariantID,
		GameName:      l.GameName,
		CardName:      l.CardName,
		CardImage:     l.CardImage,
		ExpansionName: l.ExpansionName,
		VariantName:   l.VariantName,
		Quantity:      l.Quantity,
		PriceUSD:      l.PriceUSD,
		PriceCOP:      l.PriceCOP,
		Status:        l.Status,
		Language:      l.Language,
		CreatedAt:     l.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:     l.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}
}

// List devuelve los listings del usuario autenticado.
//
//	@Summary      Listar listings
//	@Tags         listings
//	@Produce      json
//	@Security     BearerAuth
//	@Param        limit   query     int  false  "Límite"  default(50)
//	@Param        offset  query     int  false  "Offset"  default(0)
//	@Success      200     {array}   listingResponse
//	@Failure      401     {object}  object{error=string}
//	@Failure      500     {object}  object{error=string}
//	@Router       /listings [get]
func (h *ListingHandler) List(w http.ResponseWriter, r *http.Request) {
	user, ok := AuthFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	listings, err := h.list.Execute(r.Context(), user.ID, limit, offset)
	if err != nil {
		Error(w, http.StatusInternalServerError, "error interno del servidor")
		return
	}

	resp := make([]listingResponse, 0, len(listings))
	for _, l := range listings {
		resp = append(resp, newListingResponse(l))
	}
	JSON(w, http.StatusOK, resp)
}

// Create crea un listing nuevo para el usuario autenticado.
//
//	@Summary      Crear listing
//	@Tags         listings
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      createListingRequest  true  "Datos del listing"
//	@Success      201   {object}  listingResponse
//	@Failure      400   {object}  object{error=string}
//	@Failure      401   {object}  object{error=string}
//	@Failure      500   {object}  object{error=string}
//	@Router       /listings [post]
func (h *ListingHandler) Create(w http.ResponseWriter, r *http.Request) {
	user, ok := AuthFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var body createListingRequest
	if err := Decode(r, &body); err != nil {
		Error(w, http.StatusBadRequest, "body inválido")
		return
	}
	if body.VariantID <= 0 || body.Quantity <= 0 || body.PriceUSD <= 0 {
		Error(w, http.StatusBadRequest, "variant_id, quantity y price_usd son obligatorios")
		return
	}
	if body.Language == "" {
		body.Language = "Inglés"
	}

	l, err := h.create.Execute(r.Context(), listing.CreateInput{
		SellerID:  user.ID,
		VariantID: body.VariantID,
		Quantity:  body.Quantity,
		PriceUSD:  body.PriceUSD,
		Language:  body.Language,
	})
	if err != nil {
		Error(w, http.StatusInternalServerError, "error al crear listing")
		return
	}

	JSON(w, http.StatusCreated, newListingResponse(l))
}

// UpdateStock ajusta la cantidad de un listing del vendedor autenticado.
// Si la cantidad queda en 0 el listing pasa a 'inactive'; al subir stock
// vuelve a 'active'.
//
//	@Summary      Actualizar cantidad de un listing
//	@Tags         listings
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id    path  int                true  "ID del listing"
//	@Param        body  body  object{quantity=int}  true  "Nueva cantidad (0 = inactivo)"
//	@Success      200   {object}  listingResponse
//	@Failure      400   {object}  object{error=string}
//	@Failure      401   {object}  object{error=string}
//	@Failure      404   {object}  object{error=string}
//	@Router       /listings/{id} [patch]
func (h *ListingHandler) UpdateStock(w http.ResponseWriter, r *http.Request) {
	user, ok := AuthFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		Error(w, http.StatusBadRequest, "id inválido")
		return
	}

	var body struct {
		Quantity *int `json:"quantity"`
	}
	if err := Decode(r, &body); err != nil || body.Quantity == nil {
		Error(w, http.StatusBadRequest, "quantity es requerido")
		return
	}

	l, err := h.update.Execute(r.Context(), listing.UpdateStockInput{
		ID:       id,
		SellerID: user.ID,
		Quantity: *body.Quantity,
	})
	if err != nil {
		if errors.Is(err, listing.ErrNotFound) {
			Error(w, http.StatusNotFound, err.Error())
			return
		}
		Error(w, http.StatusBadRequest, err.Error())
		return
	}

	JSON(w, http.StatusOK, newListingResponse(l))
}

// Delete elimina un listing del usuario autenticado.
//
//	@Summary      Eliminar listing
//	@Tags         listings
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id   path      int  true  "ID del listing"
//	@Success      204
//	@Failure      401   {object}  object{error=string}
//	@Failure      404   {object}  object{error=string}
//	@Failure      500   {object}  object{error=string}
//	@Router       /listings/{id} [delete]
func (h *ListingHandler) Delete(w http.ResponseWriter, r *http.Request) {
	user, ok := AuthFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		Error(w, http.StatusBadRequest, "id inválido")
		return
	}

	if err := h.delete.Execute(r.Context(), id, user.ID); err != nil {
		Error(w, http.StatusNotFound, "listing no encontrado")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
