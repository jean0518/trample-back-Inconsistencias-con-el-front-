package dashboard

import (
	"context"
	"testing"
	"time"

	"trample-back/internal/domain/dashboard"
)

// fakeDashboardRepo devuelve datos fijos para comprobar cómo los arma el caso de
// uso, sin tocar la base de datos.
type fakeDashboardRepo struct {
	kpis     dashboard.KPIs
	totals   dashboard.SalesTotals
	series   []dashboard.DayCount
	recent   []dashboard.RecentOrder
	gotSince time.Time
	gotToday time.Time
	lastLim  int
}

func (f *fakeDashboardRepo) InventoryKPIs(context.Context) (dashboard.KPIs, error) {
	return f.kpis, nil
}

func (f *fakeDashboardRepo) SalesTotals(_ context.Context, since, todayStart time.Time) (dashboard.SalesTotals, error) {
	// Se recuerdan los dos cortes para verificar que la ventana nazca a
	// medianoche del día más viejo.
	f.gotSince = since
	f.gotToday = todayStart
	return f.totals, nil
}

func (f *fakeDashboardRepo) SalesSeries(context.Context, time.Time) ([]dashboard.DayCount, error) {
	return f.series, nil
}

func (f *fakeDashboardRepo) RecentSales(_ context.Context, limit int) ([]dashboard.RecentOrder, error) {
	f.lastLim = limit
	return f.recent, nil
}

// newTestUseCase fija el reloj para que las fechas de la serie sean
// deterministas.
func newTestUseCase(repo *fakeDashboardRepo, now time.Time) *SummaryUseCase {
	uc := NewSummaryUseCase(repo)
	uc.now = func() time.Time { return now }
	return uc
}

var testNow = time.Date(2026, 3, 20, 15, 30, 0, 0, time.UTC)

func TestExecuteMapeaLosKPIs(t *testing.T) {
	repo := &fakeDashboardRepo{
		kpis: dashboard.KPIs{CatalogCards: 120, ActiveListings: 8, InventoryUnits: 45},
	}
	uc := newTestUseCase(repo, testNow)

	got, err := uc.Execute(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}

	if got.CatalogCards != 120 || got.ActiveListings != 8 || got.InventoryUnits != 45 {
		t.Fatalf("KPIs no mapeados: %+v", got)
	}
}

func TestExecuteUsa14DiasY10PedidosPorDefecto(t *testing.T) {
	repo := &fakeDashboardRepo{}
	uc := newTestUseCase(repo, testNow)

	got, err := uc.Execute(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}

	if got.SeriesDays != dashboard.DefaultSeriesDays {
		t.Fatalf("esperaba %d días por defecto, hay %d", dashboard.DefaultSeriesDays, got.SeriesDays)
	}
	if len(got.Series) != dashboard.DefaultSeriesDays {
		t.Fatalf("la serie debe traer un día por cada barra, hay %d", len(got.Series))
	}
	if repo.lastLim != DefaultRecentOrders {
		t.Fatalf("esperaba %d pedidos recientes, pidió %d", DefaultRecentOrders, repo.lastLim)
	}
}

func TestExecuteAcotaDaysYRecent(t *testing.T) {
	tests := []struct {
		name       string
		days       int
		recent     int
		wantDays   int
		wantRecent int
	}{
		{"acota el máximo de días", 5000, 0, dashboard.MaxSeriesDays, DefaultRecentOrders},
		{"acota el mínimo de días", -3, 0, dashboard.DefaultSeriesDays, DefaultRecentOrders},
		{"respeta un valor válido", 7, 25, 7, 25},
		{"acota el máximo de pedidos", 7, 5000, 7, MaxRecentOrders},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeDashboardRepo{}
			uc := newTestUseCase(repo, testNow)

			got, err := uc.Execute(context.Background(), tt.days, tt.recent)
			if err != nil {
				t.Fatalf("error inesperado: %v", err)
			}
			if got.SeriesDays != tt.wantDays {
				t.Fatalf("días: esperaba %d, hay %d", tt.wantDays, got.SeriesDays)
			}
			if repo.lastLim != tt.wantRecent {
				t.Fatalf("pedidos: esperaba %d, hay %d", tt.wantRecent, repo.lastLim)
			}
		})
	}
}

// El gráfico necesita una barra por día; PostgreSQL solo devuelve los días con
// ventas, así que el caso de uso tiene que rellenar los huecos.
func TestFillSeriesRellenaLosDiasSinPedidos(t *testing.T) {
	repo := &fakeDashboardRepo{
		series: []dashboard.DayCount{
			{Date: "2026-03-19", Orders: 4, TotalCOP: 1000},
		},
	}
	uc := newTestUseCase(repo, testNow)

	got, err := uc.Execute(context.Background(), 3, 0)
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}

	want := []struct {
		date   string
		orders int
	}{
		{"2026-03-18", 0},
		{"2026-03-19", 4},
		{"2026-03-20", 0},
	}
	if len(got.Series) != len(want) {
		t.Fatalf("esperaba %d días en la serie, hay %d", len(want), len(got.Series))
	}
	for i, w := range want {
		if got.Series[i].Date != w.date || got.Series[i].Orders != w.orders {
			t.Fatalf("día %d: esperaba %s con %d pedidos, hay %s con %d",
				i, w.date, w.orders, got.Series[i].Date, got.Series[i].Orders)
		}
	}
}

// La ventana debe empezar a medianoche del día más viejo, no "hace 14 días a
// esta hora", o el primer día de la serie quedaría a medias y el conteo de
// "pedidos de hoy" no coincidiría con la serie.
func TestExecuteArrancaLaVentanaAMedianoche(t *testing.T) {
	repo := &fakeDashboardRepo{}
	uc := newTestUseCase(repo, testNow)

	if _, err := uc.Execute(context.Background(), 14, 0); err != nil {
		t.Fatalf("error inesperado: %v", err)
	}

	wantToday := time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC)
	if !repo.gotToday.Equal(wantToday) {
		t.Fatalf("hoy debe ser %s, el repo recibió %s", wantToday, repo.gotToday)
	}

	// 14 días de serie arrancan 13 días antes de hoy.
	wantSince := time.Date(2026, 3, 7, 0, 0, 0, 0, time.UTC)
	if !repo.gotSince.Equal(wantSince) {
		t.Fatalf("la ventana debe arrancar en %s, arrancó en %s", wantSince, repo.gotSince)
	}
}
