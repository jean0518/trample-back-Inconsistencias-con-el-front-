package http

import (
	"errors"
	"net/http"
	"strconv"

	appDashboard "trample-back/internal/application/dashboard"
)

type DashboardHandler struct {
	summary   *appDashboard.SummaryUseCase
	breakdown *appDashboard.BreakdownUseCase
}

func NewDashboardHandler(summary *appDashboard.SummaryUseCase, breakdown *appDashboard.BreakdownUseCase) *DashboardHandler {
	return &DashboardHandler{summary: summary, breakdown: breakdown}
}

// GetSummary devuelve en una sola llamada todo lo que muestra el panel
// "Resumen": los KPI de inventario, el conteo de pedidos, la serie de los
// últimos días y la tabla de pedidos recientes.
//
// Antes cada tarjeta del panel bajaba su listado completo y lo contaba en el
// navegador; aquí los agregados se resuelven en PostgreSQL.
//
//	@Summary      Resumen del panel (KPIs y pedidos recientes)
//	@Tags         admin
//	@Produce      json
//	@Security     BearerAuth
//	@Param        days    query  int  false  "Días de la serie"     default(14)
//	@Param        recent  query  int  false  "Pedidos recientes"    default(10)
//	@Success      200  {object}  dashboard.Summary
//	@Failure      401  {object}  object{error=string}
//	@Failure      403  {object}  object{error=string}
//	@Failure      500  {object}  object{error=string}
//	@Router       /admin/dashboard/summary [get]
func (h *DashboardHandler) GetSummary(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	recent, _ := strconv.Atoi(r.URL.Query().Get("recent"))

	summary, err := h.summary.Execute(r.Context(), days, recent)
	if err != nil {
		Error(w, http.StatusInternalServerError, friendlyErr(err))
		return
	}

	JSON(w, http.StatusOK, summary)
}

// GetSalesBreakdown devuelve el desglose de ventas de un período: totales y
// comparación con el período anterior, cartas vs. sellado, serie, top,
// desgloses por pago, entrega e idioma, y el mapa de calor por día y hora.
// Todo en hora de Colombia y solo con ventas pagadas (pendientes y canceladas
// van aparte).
//
//	@Summary      Desglose de ventas por período
//	@Tags         admin
//	@Produce      json
//	@Security     BearerAuth
//	@Param        from  query  string  false  "Desde (YYYY-MM-DD). Sin fechas: últimos 30 días"
//	@Param        to    query  string  false  "Hasta, inclusive (YYYY-MM-DD)"
//	@Param        top   query  int     false  "Tamaño del top"  default(10)
//	@Success      200  {object}  dashboard.Breakdown
//	@Failure      400  {object}  object{error=string}
//	@Failure      403  {object}  object{error=string}
//	@Router       /admin/dashboard/sales-breakdown [get]
func (h *DashboardHandler) GetSalesBreakdown(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	top, _ := strconv.Atoi(q.Get("top"))
	b, err := h.breakdown.Execute(r.Context(), q.Get("from"), q.Get("to"), top)
	if errors.Is(err, appDashboard.ErrInvalidRange) {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	JSON(w, http.StatusOK, b)
}
