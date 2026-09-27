package out

import (
	"context"
	"time"

	"trample-back/internal/domain/dashboard"
)

// DashboardRepository arma el resumen del panel administrativo con agregados en
// SQL. Existe para que el frontend no tenga que descargar el historial de
// ventas ni el de listings solo para contarlos en el navegador.
type DashboardRepository interface {
	// InventoryKPIs cuenta las cartas del catálogo, los listings activos y las
	// unidades que suman esos listings.
	InventoryKPIs(ctx context.Context) (dashboard.KPIs, error)

	// SalesTotals cuenta los pedidos de la ventana [since, ∞) y los dos cortes
	// que pide el panel: los de hoy (desde todayStart) y los de los últimos
	// siete días.
	SalesTotals(ctx context.Context, since, todayStart time.Time) (dashboard.SalesTotals, error)

	// SalesSeries agrupa los pedidos por día dentro de [since, ∞). Solo
	// devuelve los días que tuvieron pedidos; el caso de uso completa los
	// huecos para que el gráfico tenga una barra por día.
	SalesSeries(ctx context.Context, since time.Time) ([]dashboard.DayCount, error)

	// RecentSales devuelve los pedidos más recientes con su comprador y el
	// conteo de renglones, del más nuevo al más viejo.
	RecentSales(ctx context.Context, limit int) ([]dashboard.RecentOrder, error)
}
