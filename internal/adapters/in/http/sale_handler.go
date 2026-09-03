package http

import (
	"errors"
	"net/http"
	"strconv"

	appSale "trample-back/internal/application/sale"
	"trample-back/internal/domain/auth"
	"trample-back/internal/domain/sale"
)

type SaleHandler struct {
	confirm *appSale.ConfirmSaleUseCase
	list    *appSale.ListSalesUseCase
	stats   *appSale.SaleStatsUseCase
}

func NewSaleHandler(confirm *appSale.ConfirmSaleUseCase, list *appSale.ListSalesUseCase, stats *appSale.SaleStatsUseCase) *SaleHandler {
	return &SaleHandler{confirm: confirm, list: list, stats: stats}
}

type saleItemResponse struct {
	ListingID  *int64  `json:"listing_id"`
	CardID     int64   `json:"card_id"`
	CardName   string  `json:"card_name"`
	Language   string  `json:"language"`
	Quantity   int     `json:"quantity"`
	PriceCOP   int64   `json:"price_cop"`
	PriceUSD   float64 `json:"price_usd"`
}

type saleResponse struct {
	ID            int64             `json:"id"`
	TotalCOP      int64             `json:"total_cop"`
	TotalUSD      float64           `json:"total_usd"`
	ShippingCOP   int64             `json:"shipping_cop"`
	ShippingUSD   float64           `json:"shipping_usd"`
	Fulfillment   string            `json:"fulfillment"`
	Address       string            `json:"address"`
	City          string            `json:"city"`
	Phone         string            `json:"phone"`
	PaymentMethod string            `json:"payment_method"`
	Status        string            `json:"status"`
	CreatedAt     string            `json:"created_at"`
	Items         []saleItemResponse `json:"items"`
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
		ID:            s.ID,
		TotalCOP:      s.TotalCOP,
		TotalUSD:      s.TotalUSD,
		ShippingCOP:   s.ShippingCOP,
		ShippingUSD:   s.ShippingUSD,
		Fulfillment:   s.Fulfillment,
		Address:       s.Address,
		City:          s.City,
		Phone:         s.Phone,
		PaymentMethod: s.PaymentMethod,
		Status:        s.Status,
		CreatedAt:     s.CreatedAt,
		Items:         items,
	}
}

type confirmSaleRequest struct {
	ReservationIDs []int64 `json:"reservation_ids"`
	Fulfillment    string  `json:"fulfillment"`    // pickup | shipping
	Address        string  `json:"address"`        // requerido si shipping
	City           string  `json:"city"`           // requerido si shipping
	Phone          string  `json:"phone"`          // requerido si shipping
	PaymentMethod  string  `json:"payment_method"` // efectivo | transferencia
}

// ConfirmSale registra una venta local confirmando las reservas del carrito.
// Solo el administrador puede elegir efectivo; el resto paga por
// transferencia.
//
//	@Summary      Confirmar venta
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
	isAdmin := user.Role == auth.RoleAdmin

	var body confirmSaleRequest
	if err := Decode(r, &body); err != nil {
		Error(w, http.StatusBadRequest, "body inválido")
		return
	}

	created, err := h.confirm.Execute(r.Context(), sale.ConfirmInput{
		UserID:        user.ID,
		Fulfillment:   body.Fulfillment,
		Address:       body.Address,
		City:          body.City,
		Phone:         body.Phone,
		PaymentMethod: body.PaymentMethod,
		IsAdmin:       isAdmin,
	}, body.ReservationIDs)
	if err != nil {
		switch {
		case errors.Is(err, sale.ErrEmptyCart):
			Error(w, http.StatusBadRequest, "el carrito no tiene items para confirmar")
		case errors.Is(err, sale.ErrInvalidInput):
			Error(w, http.StatusBadRequest, "datos de la venta inválidos")
		case errors.Is(err, sale.ErrReservationExpired):
			Error(w, http.StatusConflict, err.Error())
		default:
			Error(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	JSON(w, http.StatusCreated, newSaleResponse(created))
}

// ListSales devuelve el historial: el admin ve todas, el cliente solo las
// suyas.
//
//	@Summary      Listar ventas
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

	var (
		items []sale.Sale
		err   error
	)
	if user.Role == auth.RoleAdmin {
		items, err = h.list.AllSales(r.Context(), limit, offset)
	} else {
		items, err = h.list.MySales(r.Context(), user.ID, limit, offset)
	}
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
	Period   string              `json:"period"`
	Buckets  []statBucketResponse `json:"buckets"`
	Orders   int                 `json:"orders"`
	TotalCOP int64               `json:"total_cop"`
	TotalUSD float64             `json:"total_usd"`
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
	period := r.URL.Query().Get("period")
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
