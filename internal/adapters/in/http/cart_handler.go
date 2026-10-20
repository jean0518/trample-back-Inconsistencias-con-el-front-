package http

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	appReservation "trample-back/internal/application/reservation"
	"trample-back/internal/domain/reservation"
	"trample-back/internal/domain/sale"
)

type CartHandler struct {
	cart *appReservation.CartUseCase
}

func NewCartHandler(cart *appReservation.CartUseCase) *CartHandler {
	return &CartHandler{cart: cart}
}

type cartItemResponse struct {
	ID          int64  `json:"id"`
	ListingID   int64  `json:"listing_id"`
	CardID      int64  `json:"card_id"`
	CardName    string `json:"card_name"`
	VariantName string `json:"variant_name"`
	Language    string `json:"language"` // idioma de la carta en el carrito
	Quantity    int    `json:"quantity"`
	// MaxQuantity es el tope del selector de cantidad: stock comprable para
	// esta línea, contando lo que ya aparta.
	MaxQuantity int     `json:"max_quantity"`
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
		MaxQuantity: r.MaxQuantity,
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
		internalError(w, err)
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
		internalError(w, err)
		return
	}

	resp := make([]cartItemResponse, 0, len(items))
	for _, it := range items {
		resp = append(resp, newCartItemResponse(it))
	}
	JSON(w, http.StatusOK, resp)
}

type updateCartItemRequest struct {
	Quantity int `json:"quantity"`
}

// UpdateCartItem cambia la cantidad de una línea del carrito. No renueva los 5
// minutos de la reserva; bajar la cantidad libera stock.
//
//	@Summary      Editar cantidad en el carrito
//	@Tags         cart
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id    path      int                    true  "ID de la reserva"
//	@Param        body  body      updateCartItemRequest  true  "Nueva cantidad (≥ 1)"
//	@Success      200   {object}  cartItemResponse
//	@Failure      400   {object}  object{error=string}
//	@Failure      401   {object}  object{error=string}
//	@Failure      404   {object}  object{error=string}
//	@Failure      409   {object}  object{error=string}  "Stock insuficiente o pago en curso"
//	@Router       /cart/{id} [patch]
func (h *CartHandler) UpdateCartItem(w http.ResponseWriter, r *http.Request) {
	user, ok := AuthFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		Error(w, http.StatusBadRequest, "id inválido")
		return
	}
	var body updateCartItemRequest
	if err := Decode(r, &body); err != nil {
		Error(w, http.StatusBadRequest, "body inválido")
		return
	}
	if body.Quantity < 1 {
		Error(w, http.StatusBadRequest, "quantity debe ser al menos 1")
		return
	}

	res, err := h.cart.UpdateQuantity(r.Context(), user.ID, id, body.Quantity)
	switch {
	case errors.Is(err, reservation.ErrInsufficientStock):
		Error(w, http.StatusConflict, "stock insuficiente para esta carta e idioma")
	case errors.Is(err, reservation.ErrCheckoutInProgress):
		Error(w, http.StatusConflict, "hay un pago en curso con esta carta")
	case errors.Is(err, reservation.ErrNotFound):
		Error(w, http.StatusNotFound, "reserva no encontrada")
	case errors.Is(err, reservation.ErrInvalidQuantity):
		Error(w, http.StatusBadRequest, "quantity debe ser al menos 1")
	case err != nil:
		internalError(w, err)
	default:
		JSON(w, http.StatusOK, newCartItemResponse(res))
	}
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
		internalError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListReservationLogs devuelve el historial de reservas de cartas y de
// productos sellados. Hay una fila por evento (no por reserva): ver
// reservation.ReservationLog. El total sin paginar va en X-Total-Count.
//
//	@Summary      Historial de reservas
//	@Tags         admin
//	@Produce      json
//	@Security     BearerAuth
//	@Param        from    query  string  false  "Desde (YYYY-MM-DD, hora Colombia)"
//	@Param        to      query  string  false  "Hasta, inclusive (YYYY-MM-DD)"
//	@Param        status  query  string  false  "reserved | returned | sold"
//	@Param        kind    query  string  false  "card | product"
//	@Param        q       query  string  false  "Cliente (nombre o correo), carta o producto"
//	@Param        limit   query  int     false  "Límite (default 100, máx. 500)"
//	@Param        offset  query  int     false  "Offset (default 0)"
//	@Success      200  {array}   reservationLogResponse
//	@Header       200  {integer} X-Total-Count  "Total de resultados"
//	@Failure      400  {object}  object{error=string}
//	@Failure      401  {object}  object{error=string}
//	@Failure      500  {object}  object{error=string}
//	@Router       /admin/reservation-logs [get]
func (h *CartHandler) ListReservationLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from, to, err := sale.ParseDateRange(q.Get("from"), q.Get("to"))
	if err != nil {
		Error(w, http.StatusBadRequest, "fechas inválidas: usa YYYY-MM-DD y que 'to' no sea anterior a 'from'")
		return
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))

	logs, total, err := h.cart.ListLogs(r.Context(), reservation.LogFilter{
		From: from, To: to,
		Status: q.Get("status"),
		Kind:   q.Get("kind"),
		Query:  q.Get("q"),
		Limit:  limit, Offset: offset,
	})
	if errors.Is(err, reservation.ErrInvalidLogFilter) {
		Error(w, http.StatusBadRequest, "status admite reserved|returned|sold y kind admite card|product")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}

	resp := make([]reservationLogResponse, 0, len(logs))
	for _, l := range logs {
		item := reservationLogResponse{
			ID:            l.ID,
			Kind:          l.Kind,
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
			ReturnReason:  l.ReturnReason,
			CreatedAt:     l.CreatedAt.UTC().Format(time.RFC3339),
			InCheckout:    l.InCheckout,
		}
		if l.Kind == reservation.KindProduct {
			id := l.ProductID
			item.ProductID = &id
			item.Title = l.Title
		}
		if !l.ExpiresAt.IsZero() {
			item.ExpiresAt = l.ExpiresAt.UTC().Format(time.RFC3339)
		}
		resp = append(resp, item)
	}
	w.Header().Set("X-Total-Count", strconv.Itoa(total))
	JSON(w, http.StatusOK, resp)
}

type reservationLogResponse struct {
	ID int64 `json:"id"`
	// Kind es card o product. Una reserva se identifica por (kind, reservation_id):
	// los ids de reservas de cartas y de productos son independientes.
	Kind          string  `json:"kind"`
	ReservationID int64   `json:"reservation_id"`
	UserID        string  `json:"user_id"`
	CustomerName  string  `json:"customer_name"`
	CustomerEmail string  `json:"customer_email"`
	CardID        int64   `json:"card_id"`
	CardName      string  `json:"card_name"`
	VariantName   string  `json:"variant_name"`
	Language      string  `json:"language"`
	ProductID     *int64  `json:"product_id,omitempty"`
	Title         string  `json:"title,omitempty"`
	Quantity      int     `json:"quantity"`
	PriceUSD      float64 `json:"price_usd"`
	PriceCOP      float64 `json:"price_cop"`
	Status        string  `json:"status"`
	// ReturnReason explica un returned: removed | expired | payment_failed.
	ReturnReason string `json:"return_reason,omitempty"`
	CreatedAt    string `json:"created_at"`
	// ExpiresAt es cuándo vence la reserva si sigue activa (RFC3339).
	ExpiresAt string `json:"expires_at,omitempty"`
	// InCheckout indica que la reserva la retiene un pago en línea abierto.
	InCheckout bool `json:"in_checkout"`
}
