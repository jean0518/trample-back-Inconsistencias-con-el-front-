package http

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	appSale "trample-back/internal/application/sale"
	"trample-back/internal/domain/auth"
	"trample-back/internal/domain/payment"
	"trample-back/internal/domain/sale"
)

type SaleHandler struct {
	confirm     *appSale.ConfirmSaleUseCase
	checkout    *appSale.CreateCheckoutUseCase
	list        *appSale.ListSalesUseCase
	stats       *appSale.SaleStatsUseCase
	checkStatus *appSale.CheckPaymentStatusUseCase
}

func NewSaleHandler(
	confirm *appSale.ConfirmSaleUseCase,
	checkout *appSale.CreateCheckoutUseCase,
	list *appSale.ListSalesUseCase,
	stats *appSale.SaleStatsUseCase,
	checkStatus *appSale.CheckPaymentStatusUseCase,
) *SaleHandler {
	return &SaleHandler{confirm: confirm, checkout: checkout, list: list, stats: stats, checkStatus: checkStatus}
}

// canSeeAllSales indica si el usuario puede consultar el historial completo de
// ventas: admins y el staff con acceso al panel de ventas, de pedidos o de
// resumen (el panel "Resumen" cuenta los pedidos recientes, así que el
// permiso 'resumen' también lo habilita).
func canSeeAllSales(user AuthUser) bool {
	if user.Role == auth.RoleAdmin || user.Role == auth.RoleColaborador || user.Role == auth.RoleSupColaborador {
		return true
	}
	for _, p := range user.Permissions {
		if p == auth.PermVentas || p == auth.PermPedidos || p == auth.PermResumen {
			return true
		}
	}
	return false
}

type saleItemResponse struct {
	ListingID *int64  `json:"listing_id"`
	CardID    int64   `json:"card_id"`
	CardName  string  `json:"card_name"`
	Language  string  `json:"language"`
	Quantity  int     `json:"quantity"`
	PriceCOP  int64   `json:"price_cop"`
	PriceUSD  float64 `json:"price_usd"`
}

type saleResponse struct {
	ID            int64   `json:"id"`
	TotalCOP      int64   `json:"total_cop"`
	TotalUSD      float64 `json:"total_usd"`
	ShippingCOP   int64   `json:"shipping_cop"`
	ShippingUSD   float64 `json:"shipping_usd"`
	Fulfillment   string  `json:"fulfillment"`
	Address       string  `json:"address"`
	City          string  `json:"city"`
	Phone         string  `json:"phone"`
	PaymentMethod string  `json:"payment_method"`
	Status        string  `json:"status"`
	CreatedAt     string  `json:"created_at"`
	// Datos del pago en línea (vacíos en efectivo o transferencia).
	PaymentReference string             `json:"payment_reference,omitempty"`
	BoldPaymentID    string             `json:"bold_payment_id,omitempty"`
	PaidAt           string             `json:"paid_at,omitempty"`
	RequiresReview   bool               `json:"requires_review"`
	Items            []saleItemResponse `json:"items"`
}

// boldCheckoutResponse son los datos que el frontend necesita para pintar el
// botón de pagos. La llave de identidad es pública; la de integridad la calculó
// el backend con la llave secreta, que nunca sale del servidor.
type boldCheckoutResponse struct {
	Reference          string `json:"reference"`
	AmountCOP          int64  `json:"amount_cop"`
	Currency           string `json:"currency"`
	IntegritySignature string `json:"integrity_signature"`
	IdentityKey        string `json:"identity_key"`
	RedirectionURL     string `json:"redirection_url"`
	OriginURL          string `json:"origin_url"`
	Description        string `json:"description"`
	// ExpirationNS es la expiración en nanosegundos epoch (formato de
	// data-expiration-date). Se alinea con la retención del stock: cuando el
	// checkout se cierra por tiempo, la pasarela también deja de aceptar el pago.
	ExpirationNS int64 `json:"expiration_ns"`
	// CustomerData y BillingAddress son objetos JSON ya serializados que Bold
	// precarga en su formulario para que el comprador no los escriba de nuevo.
	CustomerData   string `json:"customer_data,omitempty"`
	BillingAddress string `json:"billing_address,omitempty"`
}

func newBoldCheckoutResponse(c payment.Checkout) *boldCheckoutResponse {
	return &boldCheckoutResponse{
		Reference:          c.Reference,
		AmountCOP:          c.AmountCOP,
		Currency:           c.Currency,
		IntegritySignature: c.IntegritySignature,
		IdentityKey:        c.IdentityKey,
		RedirectionURL:     c.RedirectionURL,
		OriginURL:          c.OriginURL,
		Description:        c.Description,
		ExpirationNS:       c.ExpirationNS,
		CustomerData:       c.CustomerData,
		BillingAddress:     c.BillingAddress,
	}
}

func newSaleItemResponse(it sale.SaleItem) saleItemResponse {
	return saleItemResponse{
		ListingID: it.ListingID,
		CardID:    it.CardID,
		CardName:  it.CardName,
		Language:  it.Language,
		Quantity:  it.Quantity,
		PriceCOP:  it.PriceCOP,
		PriceUSD:  it.PriceUSD,
	}
}

func newSaleResponse(s sale.Sale) saleResponse {
	items := make([]saleItemResponse, 0, len(s.Items))
	for _, it := range s.Items {
		items = append(items, newSaleItemResponse(it))
	}
	return saleResponse{
		ID:               s.ID,
		TotalCOP:         s.TotalCOP,
		TotalUSD:         s.TotalUSD,
		ShippingCOP:      s.ShippingCOP,
		ShippingUSD:      s.ShippingUSD,
		Fulfillment:      s.Fulfillment,
		Address:          s.Address,
		City:             s.City,
		Phone:            s.Phone,
		PaymentMethod:    s.PaymentMethod,
		Status:           s.Status,
		CreatedAt:        s.CreatedAt,
		PaymentReference: s.PaymentReference,
		BoldPaymentID:    s.BoldPaymentID,
		PaidAt:           formatTime(s.PaidAt),
		RequiresReview:   s.RequiresReview,
		Items:            items,
	}
}

// formatTime devuelve el instante en RFC3339, o cadena vacía si no existe
// (una venta en efectivo nunca tiene fecha de pago).
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

type confirmSaleRequest struct {
	ReservationIDs []int64 `json:"reservation_ids"`
	Fulfillment    string  `json:"fulfillment"`    // pickup | shipping
	Address        string  `json:"address"`        // requerido si shipping
	City           string  `json:"city"`           // requerido si shipping
	Phone          string  `json:"phone"`          // requerido si shipping
	PaymentMethod  string  `json:"payment_method"` // efectivo | transferencia
}

// confirmInputFromRequest traduce el body a la entrada del dominio. PayerEmail
// y PayerName salen de la sesión, nunca del body: prellenan el formulario de la
// pasarela sin confiar en lo que mande el cliente.
func confirmInputFromRequest(r *http.Request, user AuthUser, body confirmSaleRequest, isStaff bool) sale.ConfirmInput {
	return sale.ConfirmInput{
		UserID:        user.ID,
		Fulfillment:   body.Fulfillment,
		Address:       body.Address,
		City:          body.City,
		Phone:         body.Phone,
		PaymentMethod: body.PaymentMethod,
		IsAdmin:       isStaff,
		PayerEmail:    user.Email,
		PayerName:     strings.TrimSpace(user.FirstName + " " + user.LastName),
	}
}

// ConfirmSale registra una venta que YA está pagada: efectivo en el acto o
// transferencia confirmada por el administrador.
//
// El pago con tarjeta no se registra por aquí a propósito: el pedido se crea
// únicamente cuando la pasarela confirma el pago, mediante POST /sales/checkout
// y el webhook o la consulta de estado.
//
//	@Summary      Confirmar venta pagada (efectivo o transferencia)
//	@Tags         sales
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      confirmSaleRequest  true  "Datos de la venta"
//	@Success      201   {object}  saleResponse
//	@Failure      400   {object}  object{error=string}
//	@Failure      401   {object}  object{error=string}
//	@Failure      409   {object}  object{error=string}
//	@Router       /sales [post]
func (h *SaleHandler) ConfirmSale(w http.ResponseWriter, r *http.Request) {
	user, ok := AuthFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var body confirmSaleRequest
	if err := Decode(r, &body); err != nil {
		Error(w, http.StatusBadRequest, "body inválido")
		return
	}
	if body.PaymentMethod == sale.PaymentBold {
		Error(w, http.StatusBadRequest, "el pago con tarjeta se procesa en la pasarela: usa /sales/checkout")
		return
	}

	result, err := h.confirm.Execute(r.Context(),
		confirmInputFromRequest(r, user, body, user.Role != auth.RoleCustomer),
		body.ReservationIDs)
	if err != nil {
		respondSaleError(w, err)
		return
	}

	JSON(w, http.StatusCreated, newSaleResponse(result.Sale))
}

// checkoutResponse describe el checkout (intención de pago) que quedó listo para
// la pasarela. No es un pedido: todavía no existe ninguno.
type checkoutResponse struct {
	Reference string                `json:"reference"`
	Status    string                `json:"status"`
	TotalCOP  int64                 `json:"total_cop"`
	TotalUSD  float64               `json:"total_usd"`
	ExpiresAt string                `json:"expires_at"`
	Bold      *boldCheckoutResponse `json:"bold"`
}

// CreateCheckout deja listo el pago en línea: valida el pedido, retiene el
// stock y devuelve los datos para abrir el modal de la pasarela.
//
// NO registra ningún pedido. La respuesta lleva la referencia del checkout (el
// order-id de Bold) con la que se puede consultar el estado del pago después.
//
//	@Summary      Preparar el pago en línea (checkout de la pasarela)
//	@Tags         sales
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      confirmSaleRequest  true  "Datos del pedido"
//	@Success      201   {object}  checkoutResponse
//	@Failure      400   {object}  object{error=string}
//	@Failure      401   {object}  object{error=string}
//	@Failure      409   {object}  object{error=string}
//	@Failure      503   {object}  object{error=string}
//	@Router       /sales/checkout [post]
func (h *SaleHandler) CreateCheckout(w http.ResponseWriter, r *http.Request) {
	user, ok := AuthFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var body confirmSaleRequest
	if err := Decode(r, &body); err != nil {
		Error(w, http.StatusBadRequest, "body inválido")
		return
	}
	body.PaymentMethod = sale.PaymentBold

	result, err := h.checkout.Execute(r.Context(),
		confirmInputFromRequest(r, user, body, user.Role != auth.RoleCustomer),
		body.ReservationIDs)
	if err != nil {
		respondSaleError(w, err)
		return
	}

	JSON(w, http.StatusCreated, checkoutResponse{
		Reference: result.Checkout.Reference,
		Status:    result.Checkout.Status,
		TotalCOP:  result.Checkout.TotalCOP,
		TotalUSD:  result.Checkout.TotalUSD,
		ExpiresAt: formatTime(result.Checkout.ExpiresAt),
		Bold:      newBoldCheckoutResponse(result.Bold),
	})
}

// respondSaleError traduce los errores del dominio de ventas a respuestas HTTP.
func respondSaleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sale.ErrEmptyCart):
		Error(w, http.StatusBadRequest, "el carrito no tiene items para confirmar")
	case errors.Is(err, sale.ErrInvalidInput):
		Error(w, http.StatusBadRequest, "datos de la venta inválidos")
	case errors.Is(err, sale.ErrReservationExpired):
		Error(w, http.StatusConflict, err.Error())
	case errors.Is(err, sale.ErrPaymentNotFound):
		Error(w, http.StatusNotFound, err.Error())
	case errors.Is(err, sale.ErrForbidden):
		Error(w, http.StatusForbidden, err.Error())
	case errors.Is(err, payment.ErrNotConfigured):
		Error(w, http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, payment.ErrAmountTooLow):
		Error(w, http.StatusBadRequest, err.Error())
	default:
		Error(w, http.StatusInternalServerError, err.Error())
	}
}

type checkPaymentRequest struct {
	Reference string `json:"reference"`
}

// paymentStatusResponse es el estado de un pago. `sale` solo viene cuando el
// pago entró: hasta entonces no existe ningún pedido.
type paymentStatusResponse struct {
	Reference string        `json:"reference"`
	Status    string        `json:"status"`
	Sale      *saleResponse `json:"sale,omitempty"`
}

// CheckPaymentStatus consulta a la pasarela si el pago de un checkout ya entró.
// Si entró, el pedido se crea en el backend y se devuelve en `sale`.
//
// El frontend la llama al volver del checkout; es idempotente, así que se puede
// repetir sin miedo.
//
//	@Summary      Validar el pago de un checkout
//	@Tags         sales
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      checkPaymentRequest  true  "Referencia del pago"
//	@Success      200   {object}  paymentStatusResponse
//	@Failure      400   {object}  object{error=string}
//	@Failure      401   {object}  object{error=string}
//	@Failure      403   {object}  object{error=string}
//	@Failure      404   {object}  object{error=string}
//	@Router       /sales/payment-status [post]
func (h *SaleHandler) CheckPaymentStatus(w http.ResponseWriter, r *http.Request) {
	user, ok := AuthFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var body checkPaymentRequest
	if err := Decode(r, &body); err != nil {
		Error(w, http.StatusBadRequest, "body inválido")
		return
	}

	status, err := h.checkStatus.Execute(r.Context(), user.ID, body.Reference, canSeeAllSales(user))
	if err != nil {
		respondSaleError(w, err)
		return
	}

	resp := paymentStatusResponse{Reference: status.Reference, Status: status.Status}
	if status.Sale != nil {
		s := newSaleResponse(*status.Sale)
		resp.Sale = &s
	}
	JSON(w, http.StatusOK, resp)
}

// ListSales devuelve el historial de pedidos SOLO del usuario autenticado
// (aunque sea admin o staff, en su cuenta solo ven sus propias compras).
//
//	@Summary      Historial de pedidos del usuario
//	@Tags         sales
//	@Produce      json
//	@Security     BearerAuth
//	@Param        limit   query  int  false  "Límite"  default(50)
//	@Param        offset  query  int  false  "Offset"  default(0)
//	@Success      200  {array}  saleResponse
//	@Failure      401  {object}  object{error=string}
//	@Router       /sales [get]
func (h *SaleHandler) ListSales(w http.ResponseWriter, r *http.Request) {
	user, ok := AuthFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 {
		limit = 50
	}

	items, err := h.list.MySales(r.Context(), user.ID, limit, offset)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp := make([]saleResponse, 0, len(items))
	for _, s := range items {
		resp = append(resp, newSaleResponse(s))
	}
	JSON(w, http.StatusOK, resp)
}

// ListAdminSales devuelve el historial COMPLETO de ventas. Solo staff con
// acceso a ventas o pedidos.
//
//	@Summary      Listado completo de ventas (staff)
//	@Tags         admin
//	@Produce      json
//	@Security     BearerAuth
//	@Param        limit   query  int  false  "Límite"  default(50)
//	@Param        offset  query  int  false  "Offset"  default(0)
//	@Success      200  {array}  saleResponse
//	@Failure      401  {object}  object{error=string}
//	@Failure      403  {object}  object{error=string}
//	@Router       /admin/sales [get]
func (h *SaleHandler) ListAdminSales(w http.ResponseWriter, r *http.Request) {
	user, ok := AuthFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if !canSeeAllSales(user) {
		Error(w, http.StatusForbidden, "no tienes acceso a ventas o pedidos")
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 {
		limit = 50
	}

	items, err := h.list.AllSales(r.Context(), limit, offset)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp := make([]saleResponse, 0, len(items))
	for _, s := range items {
		resp = append(resp, newSaleResponse(s))
	}
	JSON(w, http.StatusOK, resp)
}

type statBucketResponse struct {
	Label    string  `json:"label"`
	Start    string  `json:"start"`
	Orders   int     `json:"orders"`
	TotalCOP int64   `json:"total_cop"`
	TotalUSD float64 `json:"total_usd"`
}

type salesStatsResponse struct {
	Period   string               `json:"period"`
	Buckets  []statBucketResponse `json:"buckets"`
	Orders   int                  `json:"orders"`
	TotalCOP int64                `json:"total_cop"`
	TotalUSD float64              `json:"total_usd"`
}

// SalesStats devuelve un dashboard de ventas agregadas por día (period=day)
// o por semana (period=week). Solo admin.
//
//	@Summary      Estadísticas de ventas
//	@Tags         admin
//	@Produce      json
//	@Security     BearerAuth
//	@Param        period   query  string  false  "day | week"  default(day)
//	@Param        buckets  query  int     false  "Nº de días/semanas"  default(14)
//	@Success      200  {object}  salesStatsResponse
//	@Failure      401  {object}  object{error=string}
//	@Failure      500  {object}  object{error=string}
//	@Router       /admin/sales/stats [get]
func (h *SaleHandler) SalesStats(w http.ResponseWriter, r *http.Request) {
	// Se normaliza el periodo antes de usarlo: el caso de uso aplica el mismo
	// default, pero la respuesta debe devolver el periodo con el que se
	// calculó y no el que llegó vacío desde el frontend.
	period := r.URL.Query().Get("period")
	if period != "week" {
		period = "day"
	}
	buckets, _ := strconv.Atoi(r.URL.Query().Get("buckets"))

	bucketsList, err := h.stats.Stats(r.Context(), period, buckets)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	bucketsOut := make([]statBucketResponse, 0, len(bucketsList))
	var totalOrders int
	var totalCOP int64
	var totalUSD float64
	for _, b := range bucketsList {
		bucketsOut = append(bucketsOut, statBucketResponse{
			Label:    b.Start, // el frontend formatea la etiqueta
			Start:    b.Start,
			Orders:   b.Orders,
			TotalCOP: b.TotalCOP,
			TotalUSD: b.TotalUSD,
		})
		totalOrders += b.Orders
		totalCOP += b.TotalCOP
		totalUSD += b.TotalUSD
	}

	JSON(w, http.StatusOK, salesStatsResponse{
		Period:   period,
		Buckets:  bucketsOut,
		Orders:   totalOrders,
		TotalCOP: totalCOP,
		TotalUSD: totalUSD,
	})
}
