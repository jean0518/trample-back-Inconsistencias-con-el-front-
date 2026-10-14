package http

import (
	"errors"
	"net/http"
	"time"

	appProduct "trample-back/internal/application/product"
	"trample-back/internal/domain/product"
)

// ProductCartHandler es el carrito de productos. Va aparte del de cartas
// (/cart): mismas reglas de reserva de 5 minutos, rutas y respuestas propias.
type ProductCartHandler struct {
	cart *appProduct.CartUseCase
}

// NewProductCartHandler construye el handler.
func NewProductCartHandler(cart *appProduct.CartUseCase) *ProductCartHandler {
	return &ProductCartHandler{cart: cart}
}

type productCartItemResponse struct {
	ID        int64  `json:"id"`
	ProductID int64  `json:"product_id"`
	Title     string `json:"title"`
	Quantity  int    `json:"quantity"`
	PriceCOP  int64  `json:"price_cop"`
	// LineTotalCOP es price_cop × quantity, para que el front no recalcule.
	LineTotalCOP int64  `json:"line_total_cop"`
	Image        string `json:"image"`
	ExpiresAt    string `json:"expires_at"`
}

func toProductCartItem(r product.Reservation) productCartItemResponse {
	return productCartItemResponse{
		ID: r.ID, ProductID: r.ProductID, Title: r.Title, Quantity: r.Quantity,
		PriceCOP: r.PriceCOP, LineTotalCOP: r.PriceCOP * int64(r.Quantity),
		Image: r.ImageURL, ExpiresAt: r.ExpiresAt.UTC().Format(time.RFC3339),
	}
}

// Add aparta un producto en el carrito.
//
//	@Router /cart/products [post]
func (h *ProductCartHandler) Add(w http.ResponseWriter, r *http.Request) {
	user, ok := AuthFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body struct {
		ProductID int64 `json:"product_id"`
		Quantity  int   `json:"quantity"`
	}
	if err := Decode(r, &body); err != nil {
		Error(w, http.StatusBadRequest, "body inválido")
		return
	}
	if body.ProductID <= 0 || body.Quantity <= 0 {
		Error(w, http.StatusBadRequest, "product_id y quantity son obligatorios")
		return
	}

	res, err := h.cart.Add(r.Context(), product.ReserveInput{
		UserID: user.ID, ProductID: body.ProductID, Quantity: body.Quantity,
	})
	switch {
	case errors.Is(err, product.ErrInsufficientStock):
		Error(w, http.StatusConflict, "producto agotado o sin stock suficiente")
	case errors.Is(err, product.ErrNotFound):
		Error(w, http.StatusNotFound, "producto no encontrado")
	case err != nil:
		internalError(w, err)
	default:
		JSON(w, http.StatusCreated, toProductCartItem(res))
	}
}

// List devuelve los productos apartados del usuario.
//
//	@Router /cart/products [get]
func (h *ProductCartHandler) List(w http.ResponseWriter, r *http.Request) {
	user, ok := AuthFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	items, err := h.cart.List(r.Context(), user.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	resp := make([]productCartItemResponse, 0, len(items))
	for _, it := range items {
		resp = append(resp, toProductCartItem(it))
	}
	JSON(w, http.StatusOK, resp)
}

// Remove suelta una reserva de producto.
//
//	@Router /cart/products/{id} [delete]
func (h *ProductCartHandler) Remove(w http.ResponseWriter, r *http.Request) {
	user, ok := AuthFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id, ok := parseProductID(r, "id")
	if !ok {
		Error(w, http.StatusBadRequest, "id inválido")
		return
	}
	err := h.cart.Remove(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, product.ErrNotFound):
		Error(w, http.StatusNotFound, "reserva no encontrada")
	case err != nil:
		internalError(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
