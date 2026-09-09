package http

import (
	"errors"
	"net/http"
	"strconv"

	appReservation "trample-back/internal/application/reservation"
	"trample-back/internal/domain/reservation"
)

type CartHandler struct {
	cart *appReservation.CartUseCase
}

func NewCartHandler(cart *appReservation.CartUseCase) *CartHandler {
	return &CartHandler{cart: cart}
}

type cartItemResponse struct {
	ID          int64   `json:"id"`
	ListingID   int64   `json:"listing_id"`
	CardID      int64   `json:"card_id"`
	CardName    string  `json:"card_name"`
	VariantName string  `json:"variant_name"`
	Language    string  `json:"language"` // idioma de la carta en el carrito
	Quantity    int     `json:"quantity"`
	PriceUSD    float64 `json:"price_usd"`
	PriceCOP    float64 `json:"price_cop"`
	CardImage   string  `json:"card_image"`
	ExpiresAt   string  `json:"expires_at"`
}

func newCartItemResponse(r reservation.Reservation) cartItemResponse {
	return cartItemResponse{
		ID:          r.ID,
		ListingID:   r.ListingID,
		CardID:      r.CardID,
		CardName:    r.CardName,
		VariantName: r.VariantName,
		Language:    r.Language,
		Quantity:    r.Quantity,
		PriceUSD:    r.PriceUSD,
		PriceCOP:    r.PriceCOP,
		CardImage:   r.CardImage,
		ExpiresAt:   r.ExpiresAt.Format("2006-01-02T15:04:05Z"),
	}
}

type addToCartRequest struct {
	CardID      int64  `json:"card_id"`
	Language    string `json:"language"`
	VariantName string `json:"variant_name,omitempty"`
	Quantity    int    `json:"quantity"`
}

// AddToCart agrega una carta al carrito, reservando su stock por 5 minutos.
//
//	@Summary      Agregar al carrito
//	@Tags         cart
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      addToCartRequest  true  "Carta e idioma a reservar"
//	@Success      201   {object}  cartItemResponse
//	@Failure      400   {object}  object{error=string}
//	@Failure      401   {object}  object{error=string}
//	@Failure      409   {object}  object{error=string}  "Stock insuficiente"
//	@Router       /cart [post]
func (h *CartHandler) AddToCart(w http.ResponseWriter, r *http.Request) {
	user, ok := AuthFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var body addToCartRequest
	if err := Decode(r, &body); err != nil {
		Error(w, http.StatusBadRequest, "body inválido")
		return
	}
	if body.CardID <= 0 || body.Language == "" || body.Quantity <= 0 {
		Error(w, http.StatusBadRequest, "card_id, language y quantity son obligatorios")
		return
	}

	res, err := h.cart.Add(r.Context(), reservation.ReserveInput{
		UserID:      user.ID,
		CardID:      body.CardID,
		Language:    body.Language,
		VariantName: body.VariantName,
		Quantity:    body.Quantity,
	})
	if err != nil {
		if errors.Is(err, reservation.ErrInsufficientStock) {
			Error(w, http.StatusConflict, "stock insuficiente para esta carta e idioma")
			return
		}
		if errors.Is(err, reservation.ErrInvalidListing) {
			Error(w, http.StatusBadRequest, "no hay inventario activo para esta carta e idioma")
			return
		}
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	JSON(w, http.StatusCreated, newCartItemResponse(res))
}

// GetCart devuelve el carrito (reservas activas) del usuario autenticado.
//
//	@Summary      Ver carrito
//	@Tags         cart
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {array}  cartItemResponse
//	@Failure      401  {object}  object{error=string}
//	@Router       /cart [get]
func (h *CartHandler) GetCart(w http.ResponseWriter, r *http.Request) {
	user, ok := AuthFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	items, err := h.cart.Cart(r.Context(), user.ID)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp := make([]cartItemResponse, 0, len(items))
	for _, it := range items {
		resp = append(resp, newCartItemResponse(it))
	}
	JSON(w, http.StatusOK, resp)
}

// RemoveFromCart libera una reserva del carrito y restaura el stock.
//
//	@Summary      Eliminar del carrito
//	@Tags         cart
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id   path  int  true  "ID de la reserva"
//	@Success      204
//	@Failure      401  {object}  object{error=string}
//	@Failure      404  {object}  object{error=string}
//	@Router       /cart/{id} [delete]
func (h *CartHandler) RemoveFromCart(w http.ResponseWriter, r *http.Request) {
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

	if err := h.cart.Remove(r.Context(), user.ID, id); err != nil {
		if errors.Is(err, reservation.ErrNotFound) {
			Error(w, http.StatusNotFound, "reserva no encontrada")
			return
		}
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListReservationLogs devuelve el historial de reservas del carrito. Solo
// admin. Filtro opcional por estado: reserved | returned | sold.
//
//	@Summary      Historial de reservas
//	@Tags         admin
//	@Produce      json
//	@Security     BearerAuth
//	@Param        status   query  string  false  "Filtrar por estado (reserved|returned|sold)"
//	@Param        limit    query  int     false  "Límite (default 100)"
//	@Param        offset   query  int     false  "Offset (default 0)"
//	@Success      200  {array}  reservationLogResponse
//	@Failure      401  {object}  object{error=string}
//	@Failure      500  {object}  object{error=string}
//	@Router       /admin/reservation-logs [get]
func (h *CartHandler) ListReservationLogs(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	logs, err := h.cart.ListLogs(r.Context(), r.URL.Query().Get("status"), limit, offset)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp := make([]reservationLogResponse, 0, len(logs))
	for _, l := range logs {
		resp = append(resp, reservationLogResponse{
			ID:            l.ID,
			ReservationID: l.ReservationID,
			UserID:        l.UserID,
			CustomerName:  l.CustomerName,
			CustomerEmail: l.CustomerEmail,
			CardID:        l.CardID,
			CardName:      l.CardName,
			VariantName:   l.VariantName,
			Language:      l.Language,
			Quantity:      l.Quantity,
			PriceUSD:      l.PriceUSD,
			PriceCOP:      l.PriceCOP,
			Status:        l.Status,
			CreatedAt:     l.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	JSON(w, http.StatusOK, resp)
}

type reservationLogResponse struct {
	ID            int64   `json:"id"`
	ReservationID int64   `json:"reservation_id"`
	UserID        int64   `json:"user_id"`
	CustomerName  string  `json:"customer_name"`
	CustomerEmail string  `json:"customer_email"`
	CardID        int64   `json:"card_id"`
	CardName      string  `json:"card_name"`
	VariantName   string  `json:"variant_name"`
	Language      string  `json:"language"`
	Quantity      int     `json:"quantity"`
	PriceUSD      float64 `json:"price_usd"`
	PriceCOP      float64 `json:"price_cop"`
	Status        string  `json:"status"`
	CreatedAt     string  `json:"created_at"`
}
