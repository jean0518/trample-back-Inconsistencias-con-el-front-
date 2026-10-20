package http

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

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
	pickups     *appSale.PickupOrdersUseCase
	fulfillment *appSale.FulfillmentUseCase
}

func NewSaleHandler(
	confirm *appSale.ConfirmSaleUseCase,
	checkout *appSale.CreateCheckoutUseCase,
	list *appSale.ListSalesUseCase,
	stats *appSale.SaleStatsUseCase,
	checkStatus *appSale.CheckPaymentStatusUseCase,
	pickups *appSale.PickupOrdersUseCase,
	fulfillment *appSale.FulfillmentUseCase,
) *SaleHandler {
	return &SaleHandler{confirm: confirm, checkout: checkout, list: list, stats: stats, checkStatus: checkStatus, pickups: pickups, fulfillment: fulfillment}
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
	// Snapshot al vender. Vacíos en líneas viejas cuya carta ya no existe.
	VariantName   string `json:"variant_name"`
	GameCode      string `json:"game_code"`
	GameName      string `json:"game_name"`
	ExpansionName string `json:"expansion_name"`
}

// productItemResponse es una línea de producto propio de la tienda en un pedido.
type productItemResponse struct {
	ProductID *int64 `json:"product_id"`
	Title     string `json:"title"`
	Quantity  int    `json:"quantity"`
	PriceCOP  int64  `json:"price_cop"`
}

type saleResponse struct {
	ID int64 `json:"id"`
	// Comprador (nombre + apellido y correo de su cuenta).
	CustomerName  string  `json:"customer_name"`
	CustomerEmail string  `json:"customer_email"`
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
	PaymentReference string `json:"payment_reference,omitempty"`
	BoldPaymentID    string `json:"bold_payment_id,omitempty"`
	PaidAt           string `json:"paid_at,omitempty"`
	RequiresReview   bool   `json:"requires_review"`
	// PickupDeadline es el límite para pagar y recoger un pedido pendiente en
	// tienda (RFC3339). Vacío en los demás pedidos.
	PickupDeadline string `json:"pickup_deadline,omitempty"`
	// CancelRequestedAt aparece cuando el cliente pidió cancelar un pedido de
	// recogida y el staff aún no decide (o ya lo canceló a pedido suyo).
	CancelRequestedAt string `json:"cancel_requested_at,omitempty"`
	// ShippingStatus es la etapa del despacho (pending | shipped | delivered).
	// Solo viene en los pedidos pagados con envío a domicilio: el panel usa su
	// presencia para mostrar las acciones de envío.
	ShippingStatus string `json:"shipping_status,omitempty"`
	// Despacho de los pedidos con envío (RFC3339; vacíos si no aplica).
	ShippedAt      string `json:"shipped_at,omitempty"`
	Carrier        string `json:"carrier,omitempty"`
	TrackingNumber string `json:"tracking_number,omitempty"`
	// DeliveredAt: entregado por la transportadora o recogido en la tienda.
	DeliveredAt string             `json:"delivered_at,omitempty"`
	Items       []saleItemResponse `json:"items"`
	// ProductItems son las líneas de productos propios de la tienda (siempre
	// presente; vacío si el pedido es solo de cartas).
	ProductItems []productItemResponse `json:"product_items"`
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

		VariantName:   it.VariantName,
		GameCode:      it.GameCode,
		GameName:      it.GameName,
		ExpansionName: it.ExpansionName,
	}
}

func newSaleResponse(s sale.Sale) saleResponse {
	items := make([]saleItemResponse, 0, len(s.Items))
	for _, it := range s.Items {
		items = append(items, newSaleItemResponse(it))
	}
	productItems := make([]productItemResponse, 0, len(s.ProductItems))
	for _, it := range s.ProductItems {
		productItems = append(productItems, productItemResponse{
			ProductID: it.ProductID, Title: it.Title, Quantity: it.Quantity, PriceCOP: it.PriceCOP,
		})
	}
	return saleResponse{
		ID:               s.ID,
		CustomerName:     s.CustomerName,
		CustomerEmail:    s.CustomerEmail,
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
		PickupDeadline:   formatTime(s.PickupDeadline),

		CancelRequestedAt: formatTime(s.CancelRequestedAt),
		ShippingStatus:    shippingStatus(s),
		ShippedAt:         formatTime(s.ShippedAt),
		Carrier:           s.Carrier,
		TrackingNumber:    s.TrackingNumber,
		DeliveredAt:       formatTime(s.DeliveredAt),
		Items:             items,
		ProductItems:      productItems,
	}
}

// shippingStatus deriva la etapa del despacho de un pedido pagado con envío.
func shippingStatus(s sale.Sale) string {
	if s.Fulfillment != sale.FulfillmentShipping || s.Status != sale.StatusPaid {
		return ""
	}
	switch {
	case !s.DeliveredAt.IsZero():
		return "delivered"
	case !s.ShippedAt.IsZero():
		return "shipped"
	default:
		return "pending"
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
	// ProductReservationIDs son las reservas del carrito de productos
	// (GET /cart/products) que se pagan en el mismo pedido.
	ProductReservationIDs []int64 `json:"product_reservation_ids"`
	Fulfillment           string  `json:"fulfillment"`    // pickup | shipping
	Address               string  `json:"address"`        // requerido si shipping
	City                  string  `json:"city"`           // requerido si shipping
	Phone                 string  `json:"phone"`          // requerido si shipping
	PaymentMethod         string  `json:"payment_method"` // efectivo | transferencia (staff) | tienda (recogida, se paga en el local)
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

		ProductReservationIDs: body.ProductReservationIDs,
		PayerEmail:            user.Email,
		PayerName:             strings.TrimSpace(user.FirstName + " " + user.LastName),
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
	case errors.Is(err, sale.ErrEmailNotVerified):
		// El código estable deja al frontend mostrar "verifica tu correo" con el
		// botón de reenviar, sin depender del texto.
		JSON(w, http.StatusForbidden, map[string]string{"error": err.Error(), "code": "email_not_verified"})
	case errors.Is(err, sale.ErrEmptyCart):
		Error(w, http.StatusBadRequest, "el carrito no tiene items para confirmar")
	case errors.Is(err, sale.ErrInvalidInput):
		Error(w, http.StatusBadRequest, "datos de la venta inválidos")
	case errors.Is(err, sale.ErrReservationExpired), errors.Is(err, sale.ErrCheckoutConflict),
		errors.Is(err, sale.ErrInsufficientStock), errors.Is(err, sale.ErrNotPendingPickup),
		errors.Is(err, sale.ErrCancelAlreadyRequested), errors.Is(err, sale.ErrNoCancelRequest),
		errors.Is(err, sale.ErrNotShippable), errors.Is(err, sale.ErrNotInReview):
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
		internalError(w, err)
	}
}

type checkPaymentRequest struct {
	Reference string `json:"reference"`
}

// paymentStatusResponse es el estado de un pago. `sale` solo viene cuando el
// pago entró: hasta entonces no existe ningún pedido.
type paymentStatusResponse struct {
	Reference string `json:"reference"`
	Status    string `json:"status"`
	// GatewayStatus aparece cuando el último intento fue rechazado: el checkout
	// sigue "open" (se puede reintentar) pero el frontend debe avisar del fallo.
	GatewayStatus string        `json:"gateway_status,omitempty"`
	Sale          *saleResponse `json:"sale,omitempty"`
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

	resp := paymentStatusResponse{Reference: status.Reference, Status: status.Status, GatewayStatus: status.GatewayStatus}
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
		internalError(w, err)
		return
	}

	resp := make([]saleResponse, 0, len(items))
	for _, s := range items {
		resp = append(resp, newSaleResponse(s))
	}
	JSON(w, http.StatusOK, resp)
}

// ListAdminSales devuelve el historial de ventas con filtros. Solo staff con
// acceso a ventas o pedidos. Responde un arreglo (mismo contrato de siempre) y
// el total sin paginar en el header X-Total-Count.
//
//	@Summary      Listado de ventas con filtros (staff)
//	@Tags         admin
//	@Produce      json
//	@Security     BearerAuth
//	@Param        from             query  string  false  "Desde (YYYY-MM-DD, hora Colombia)"
//	@Param        to               query  string  false  "Hasta, inclusive (YYYY-MM-DD)"
//	@Param        status           query  string  false  "paid | pending_pickup | cancelled"
//	@Param        shipping_status  query  string  false  "pending | shipped | delivered"
//	@Param        fulfillment      query  string  false  "pickup | shipping"
//	@Param        payment_method   query  string  false  "bold | efectivo | transferencia | tienda"
//	@Param        q                query  string  false  "Busca en #id, cliente, teléfono, ciudad, carta o producto"
//	@Param        sort             query  string  false  "-created_at (default) | created_at"
//	@Param        limit            query  int     false  "Límite (default 50, máx. 500)"
//	@Param        offset           query  int     false  "Offset"
//	@Success      200  {array}   saleResponse
//	@Header       200  {integer} X-Total-Count  "Total de resultados"
//	@Failure      400  {object}  object{error=string}
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
	filter, ok := saleFilterFromQuery(w, r)
	if !ok {
		return
	}

	items, total, err := h.list.Search(r.Context(), filter)
	if err != nil {
		respondSaleError(w, err)
		return
	}

	resp := make([]saleResponse, 0, len(items))
	for _, s := range items {
		resp = append(resp, newSaleResponse(s))
	}
	w.Header().Set("X-Total-Count", strconv.Itoa(total))
	JSON(w, http.StatusOK, resp)
}

// SaleCounts cuenta los pedidos por etapa para las tarjetas del panel de
// pedidos. Acepta los mismos filtros que /admin/sales salvo status.
//
//	@Summary      Conteo de pedidos por etapa (staff)
//	@Tags         admin
//	@Produce      json
//	@Security     BearerAuth
//	@Param        from  query  string  false  "Desde (YYYY-MM-DD)"
//	@Param        to    query  string  false  "Hasta, inclusive (YYYY-MM-DD)"
//	@Param        q     query  string  false  "Búsqueda"
//	@Success      200  {object}  sale.StageCounts
//	@Failure      400  {object}  object{error=string}
//	@Failure      403  {object}  object{error=string}
//	@Router       /admin/sales/counts [get]
func (h *SaleHandler) SaleCounts(w http.ResponseWriter, r *http.Request) {
	user, ok := AuthFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if !canSeeAllSales(user) {
		Error(w, http.StatusForbidden, "no tienes acceso a ventas o pedidos")
		return
	}
	filter, ok := saleFilterFromQuery(w, r)
	if !ok {
		return
	}
	counts, err := h.list.Counts(r.Context(), filter)
	if err != nil {
		respondSaleError(w, err)
		return
	}
	JSON(w, http.StatusOK, counts)
}

// saleFilterFromQuery lee los filtros del listado de ventas. Responde 400 si
// una fecha u orden no es válido.
func saleFilterFromQuery(w http.ResponseWriter, r *http.Request) (sale.ListFilter, bool) {
	q := r.URL.Query()
	from, to, err := sale.ParseDateRange(q.Get("from"), q.Get("to"))
	if err != nil {
		Error(w, http.StatusBadRequest, "fechas inválidas: usa YYYY-MM-DD y que 'to' no sea anterior a 'from'")
		return sale.ListFilter{}, false
	}
	sort := q.Get("sort")
	if sort != "" && sort != "created_at" && sort != "-created_at" {
		Error(w, http.StatusBadRequest, "sort admite created_at o -created_at")
		return sale.ListFilter{}, false
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	return sale.ListFilter{
		From:           from,
		To:             to,
		Status:         q.Get("status"),
		ShippingStatus: q.Get("shipping_status"),
		Fulfillment:    q.Get("fulfillment"),
		PaymentMethod:  q.Get("payment_method"),
		Query:          q.Get("q"),
		Ascending:      sort == "created_at",
		Limit:          limit,
		Offset:         offset,
	}, true
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
		internalError(w, err)
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

// MarkPickupPaid registra que el cliente pagó y recogió su pedido en la tienda.
//
//	@Summary      Marcar como pagado un pedido de recogida en tienda
//	@Tags         admin
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id   path      int  true  "ID del pedido"
//	@Success      200  {object}  saleResponse
//	@Failure      404  {object}  object{error=string}
//	@Failure      409  {object}  object{error=string}
//	@Router       /admin/sales/{id}/mark-paid [post]
func (h *SaleHandler) MarkPickupPaid(w http.ResponseWriter, r *http.Request) {
	id, ok := saleIDParam(w, r)
	if !ok {
		return
	}
	s, err := h.pickups.MarkPaid(r.Context(), id)
	if err != nil {
		respondSaleError(w, err)
		return
	}
	JSON(w, http.StatusOK, newSaleResponse(s))
}

// CancelPickup cancela un pedido de recogida pendiente y devuelve su stock.
//
//	@Summary      Cancelar un pedido de recogida en tienda pendiente
//	@Tags         admin
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id   path      int  true  "ID del pedido"
//	@Success      200  {object}  saleResponse
//	@Failure      404  {object}  object{error=string}
//	@Failure      409  {object}  object{error=string}
//	@Router       /admin/sales/{id}/cancel [post]
func (h *SaleHandler) CancelPickup(w http.ResponseWriter, r *http.Request) {
	id, ok := saleIDParam(w, r)
	if !ok {
		return
	}
	s, err := h.pickups.Cancel(r.Context(), id)
	if err != nil {
		respondSaleError(w, err)
		return
	}
	JSON(w, http.StatusOK, newSaleResponse(s))
}

// RequestPickupCancel deja que el cliente pida cancelar su pedido de recogida.
// No lo cancela: el staff aprueba (POST /admin/sales/{id}/cancel) o rechaza
// (POST /admin/sales/{id}/reject-cancel). Los pedidos pagados en línea no se
// cancelan desde la página: se gestionan por WhatsApp.
//
//	@Summary      Solicitar la cancelación de un pedido de recogida
//	@Tags         sales
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id   path      int  true  "ID del pedido"
//	@Success      200  {object}  saleResponse
//	@Failure      403  {object}  object{error=string}
//	@Failure      404  {object}  object{error=string}
//	@Failure      409  {object}  object{error=string}
//	@Router       /sales/{id}/cancel-request [post]
func (h *SaleHandler) RequestPickupCancel(w http.ResponseWriter, r *http.Request) {
	user, ok := AuthFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id, ok := saleIDParam(w, r)
	if !ok {
		return
	}
	s, err := h.pickups.RequestCancel(r.Context(), id, user.ID)
	if err != nil {
		respondSaleError(w, err)
		return
	}
	JSON(w, http.StatusOK, newSaleResponse(s))
}

// RejectPickupCancel rechaza la solicitud de cancelación: el pedido sigue
// apartado y el cliente recibe un correo con el contacto de la tienda.
//
//	@Summary      Rechazar la cancelación solicitada por el cliente
//	@Tags         admin
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id   path      int  true  "ID del pedido"
//	@Success      200  {object}  saleResponse
//	@Failure      404  {object}  object{error=string}
//	@Failure      409  {object}  object{error=string}
//	@Router       /admin/sales/{id}/reject-cancel [post]
func (h *SaleHandler) RejectPickupCancel(w http.ResponseWriter, r *http.Request) {
	h.adminSaleAction(w, r, h.pickups.RejectCancel)
}

// ConfirmReview marca como revisado un pedido que requería revisión (pago
// tardío o con otro monto) y le envía al cliente la confirmación del pedido.
//
//	@Summary      Confirmar un pedido en revisión
//	@Tags         admin
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id   path      int  true  "ID del pedido"
//	@Success      200  {object}  saleResponse
//	@Failure      404  {object}  object{error=string}
//	@Failure      409  {object}  object{error=string}
//	@Router       /admin/sales/{id}/confirm-review [post]
func (h *SaleHandler) ConfirmReview(w http.ResponseWriter, r *http.Request) {
	h.adminSaleAction(w, r, h.fulfillment.ConfirmReview)
}

type shipOrderRequest struct {
	Carrier        string `json:"carrier"`         // obligatorio
	TrackingNumber string `json:"tracking_number"` // opcional
}

// ShipOrder marca como enviado un pedido pagado con envío a domicilio. Un
// pedido en revisión no se puede despachar (409).
//
//	@Summary      Marcar un pedido como enviado
//	@Tags         admin
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id    path      int               true  "ID del pedido"
//	@Param        body  body      shipOrderRequest  true  "Transportadora y guía"
//	@Success      200   {object}  saleResponse
//	@Failure      400   {object}  object{error=string}
//	@Failure      404   {object}  object{error=string}
//	@Failure      409   {object}  object{error=string}
//	@Router       /admin/sales/{id}/ship [post]
func (h *SaleHandler) ShipOrder(w http.ResponseWriter, r *http.Request) {
	var body shipOrderRequest
	if err := Decode(r, &body); err != nil {
		Error(w, http.StatusBadRequest, "body inválido")
		return
	}
	h.adminSaleAction(w, r, func(ctx context.Context, id int64) (sale.Sale, error) {
		return h.fulfillment.MarkShipped(ctx, id, body.Carrier, body.TrackingNumber)
	})
}

// DeliverOrder marca como entregado un pedido ya enviado.
//
//	@Summary      Marcar un pedido enviado como entregado
//	@Tags         admin
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id   path      int  true  "ID del pedido"
//	@Success      200  {object}  saleResponse
//	@Failure      404  {object}  object{error=string}
//	@Failure      409  {object}  object{error=string}
//	@Router       /admin/sales/{id}/deliver [post]
func (h *SaleHandler) DeliverOrder(w http.ResponseWriter, r *http.Request) {
	h.adminSaleAction(w, r, h.fulfillment.MarkDelivered)
}

// adminSaleAction lee el id del pedido, aplica la acción y responde el pedido
// actualizado.
func (h *SaleHandler) adminSaleAction(w http.ResponseWriter, r *http.Request, action func(ctx context.Context, id int64) (sale.Sale, error)) {
	id, ok := saleIDParam(w, r)
	if !ok {
		return
	}
	s, err := action(r.Context(), id)
	if err != nil {
		respondSaleError(w, err)
		return
	}
	JSON(w, http.StatusOK, newSaleResponse(s))
}

func saleIDParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		Error(w, http.StatusBadRequest, "id inválido")
		return 0, false
	}
	return id, true
}
