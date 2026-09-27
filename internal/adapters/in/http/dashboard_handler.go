package http

import (
	"net/http"
	"strconv"

	appDashboard "trample-back/internal/application/dashboard"
)

type DashboardHandler struct {
	summary *appDashboard.SummaryUseCase
}

func NewDashboardHandler(summary *appDashboard.SummaryUseCase) *DashboardHandler {
	return &DashboardHandler{summary: summary}
}

// GetSummary devuelve en una sola llamada todo lo que muestra el panel
// "Resumen": los KPI de catálogo e inventario, el conteo de pedidos, la serie
// de los últimos días y la tabla de pedidos recientes.
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
