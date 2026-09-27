package dashboard

import (
	"context"
	"time"

	"trample-back/internal/domain/dashboard"
	"trample-back/internal/ports/out"
)

// Límites de la tabla de pedidos recientes del resumen.
const (
	DefaultRecentOrders = 10
	MaxRecentOrders     = 50
)

// SummaryUseCase arma la vista del panel "Resumen". Todas las consultas son
// agregados sobre índices, así que el costo no crece con el tamaño del historial
// y el frontend no necesita descargar ventas para contarlas.
type SummaryUseCase struct {
	repo out.DashboardRepository
	// now se inyecta para que las pruebas fijen la fecha en vez de depender del
	// reloj. En producción queda nil y se usa time.Now.
	now func() time.Time
}

func NewSummaryUseCase(repo out.DashboardRepository) *SummaryUseCase {
	return &SummaryUseCase{repo: repo, now: time.Now}
}

// Execute devuelve el resumen completo. days es la cantidad de días de la serie
// y recent el número de pedidos de la tabla; ambos se acotan para que el
// frontend no pueda generar una consulta desproporcionada.
func (uc *SummaryUseCase) Execute(ctx context.Context, days, recent int) (dashboard.Summary, error) {
	days = clamp(days, dashboard.DefaultSeriesDays, dashboard.MinSeriesDays, dashboard.MaxSeriesDays)
	recent = clamp(recent, DefaultRecentOrders, 1, MaxRecentOrders)

	// La ventana arranca a medianoche del día más viejo para que la serie cubra
	// exactamente los días pedidos, sin contar los del día previo que se
	// colarían por el reloj de la consulta.
	now := uc.now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	since := todayStart.AddDate(0, 0, -(days - 1))

	kpis, err := uc.repo.InventoryKPIs(ctx)
	if err != nil {
		return dashboard.Summary{}, err
	}
	totals, err := uc.repo.SalesTotals(ctx, since, todayStart)
	if err != nil {
		return dashboard.Summary{}, err
	}
	series, err := uc.repo.SalesSeries(ctx, since)
	if err != nil {
		return dashboard.Summary{}, err
	}
	recentOrders, err := uc.repo.RecentSales(ctx, recent)
	if err != nil {
		return dashboard.Summary{}, err
	}

	return dashboard.Summary{
		CatalogCards:   kpis.CatalogCards,
		ActiveListings: kpis.ActiveListings,
		InventoryUnits: kpis.InventoryUnits,
		OrdersToday:    totals.OrdersToday,
		OrdersLast7:    totals.OrdersLast7,
		PeriodOrders:   totals.Orders,
		PeriodTotalCOP: totals.TotalCOP,
		PeriodTotalUSD: totals.TotalUSD,
		SeriesDays:     days,
		Series:         fillSeries(series, since, days),
		RecentOrders:   recentOrders,
	}, nil
}

// fillSeries convierte los días con pedidos en una serie continua: PostgreSQL
// solo devuelve los días que tuvieron ventas, y el gráfico necesita una barra
// por día para que el eje no "salte" los días en cero.
func fillSeries(present []dashboard.DayCount, since time.Time, days int) []dashboard.DayCount {
	byDate := make(map[string]dashboard.DayCount, len(present))
	for _, d := range present {
		byDate[d.Date] = d
	}

	series := make([]dashboard.DayCount, 0, days)
	for i := 0; i < days; i++ {
		key := since.AddDate(0, 0, i).Format("2006-01-02")
		if d, ok := byDate[key]; ok {
			series = append(series, d)
			continue
		}
		series = append(series, dashboard.DayCount{Date: key})
	}
	return series
}

// clamp lleva el valor dentro de [min, max] y aplica def cuando el frontend no
// envió nada (0 o negativo).
func clamp(value, def, min, max int) int {
	if value <= 0 {
		value = def
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
