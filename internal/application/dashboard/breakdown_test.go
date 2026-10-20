package dashboard

import (
	"context"
	"errors"
	"testing"
	"time"

	"trample-back/internal/domain/dashboard"
	"trample-back/internal/domain/sale"
)

func at(day, hour int) time.Time { return time.Date(2026, 10, day, hour, 0, 0, 0, colombia) }

func id(v int64) *int64 { return &v }

func breakdownFixture() *fakeDashboardRepo {
	return &fakeDashboardRepo{
		current: []dashboard.BreakdownSale{
			{ID: 1, Status: sale.StatusPaid, TotalCOP: 70000, ShippingCOP: 25000, PaymentMethod: "bold", Fulfillment: "shipping",
				CreatedAt: at(5, 10), // lunes 10 a. m.
				Lines: []dashboard.BreakdownLine{
					{CardID: 7, Name: "Pikachu", Language: "Español", Quantity: 2, PriceCOP: 10000},
					{Sealed: true, ProductID: id(3), Name: "Booster", Quantity: 1, PriceCOP: 25000},
				}},
			{ID: 2, Status: sale.StatusPaid, TotalCOP: 30000, PaymentMethod: "tienda", Fulfillment: "pickup",
				CreatedAt: at(6, 18),
				Lines: []dashboard.BreakdownLine{
					{CardID: 7, Name: "Pikachu", Language: "Inglés", Quantity: 1, PriceCOP: 12000},
					{Sealed: true, ProductID: nil, Name: "Caja borrada", Quantity: 3, PriceCOP: 6000},
				}},
			{ID: 3, Status: sale.StatusPendingPickup, TotalCOP: 9000, CreatedAt: at(6, 9)},
			{ID: 4, Status: sale.StatusCancelled, TotalCOP: 4000, CreatedAt: at(7, 9)},
		},
		previous: []dashboard.BreakdownSale{
			{Status: sale.StatusPaid, TotalCOP: 50000, Lines: []dashboard.BreakdownLine{{Quantity: 4}}},
			{Status: sale.StatusCancelled, TotalCOP: 99999},
		},
	}
}

func TestBreakdown_ReglasDelPanel(t *testing.T) {
	repo := breakdownFixture()
	b, err := NewBreakdownUseCase(repo).Execute(context.Background(), "2026-10-05", "2026-10-11", 10)
	if err != nil {
		t.Fatal(err)
	}

	// Solo las pagadas suman; el total incluye el envío.
	if b.Totals != (dashboard.BreakdownTotals{RevenueCOP: 100000, Orders: 2, Units: 7, AvgTicketCOP: 50000, ShippingCOP: 25000}) {
		t.Errorf("totals = %+v", b.Totals)
	}
	// Cartas y sellado sin el envío; orders = pedidos con al menos una línea.
	if b.Cards != (dashboard.CategoryTotals{RevenueCOP: 32000, Units: 3, Orders: 2}) {
		t.Errorf("cards = %+v", b.Cards)
	}
	if b.Sealed != (dashboard.CategoryTotals{RevenueCOP: 43000, Units: 4, Orders: 2}) {
		t.Errorf("sealed = %+v", b.Sealed)
	}
	if b.Pending != (dashboard.OrdersAmount{Orders: 1, TotalCOP: 9000}) || b.Cancelled != (dashboard.OrdersAmount{Orders: 1, TotalCOP: 4000}) {
		t.Errorf("pending = %+v, cancelled = %+v", b.Pending, b.Cancelled)
	}
	if b.Previous != (dashboard.PeriodTotals{RevenueCOP: 50000, Orders: 1, Units: 4}) {
		t.Errorf("previous = %+v", b.Previous)
	}

	// El período anterior tiene el mismo largo y termina donde empieza este.
	if got := repo.ranges[1]; !got[0].Equal(at(5, 0).AddDate(0, 0, -7)) || !got[1].Equal(at(5, 0)) {
		t.Errorf("rango anterior = %v", got)
	}

	// Serie diaria con los días vacíos incluidos.
	if b.Granularity != "day" || len(b.Series) != 7 || b.Series[0].Start != "2026-10-05" || b.Series[6].Start != "2026-10-11" {
		t.Fatalf("serie = %s %+v", b.Granularity, b.Series)
	}
	if b.Series[0] != (dashboard.SeriesBucket{Start: "2026-10-05", CardsCOP: 20000, SealedCOP: 25000, Orders: 1}) {
		t.Errorf("día 5 = %+v", b.Series[0])
	}
	if b.Series[2].Orders != 0 {
		t.Errorf("el día 7 solo tuvo una cancelada: %+v", b.Series[2])
	}

	// Top: por carta e idioma; el producto borrado se agrupa por título.
	if len(b.TopCards) != 2 || b.TopCards[0].Language != "Español" || b.TopCards[0].Units != 2 {
		t.Errorf("top_cards = %+v", b.TopCards)
	}
	if len(b.TopSealed) != 2 || b.TopSealed[0].Title != "Caja borrada" || b.TopSealed[0].ProductID != nil {
		t.Errorf("top_sealed = %+v", b.TopSealed)
	}

	// Mapa de calor: lunes (fila 0) a las 10 y martes a las 18.
	if b.Heatmap[0][10] != 1 || b.Heatmap[1][18] != 1 {
		t.Errorf("heatmap lunes 10 = %d, martes 18 = %d", b.Heatmap[0][10], b.Heatmap[1][18])
	}
	if len(b.ByPayment) != 2 || b.ByPayment[0].Key != "bold" || len(b.ByLanguage) != 2 || b.ByLanguage[0].Key != "Español" {
		t.Errorf("by_payment = %+v, by_language = %+v", b.ByPayment, b.ByLanguage)
	}
}

func TestBreakdown_MensualDespuesDe45Dias(t *testing.T) {
	b, err := NewBreakdownUseCase(&fakeDashboardRepo{}).Execute(context.Background(), "2026-01-15", "2026-03-10", 0)
	if err != nil {
		t.Fatal(err)
	}
	if b.Granularity != "month" || len(b.Series) != 3 || b.Series[0].Start != "2026-01-01" || b.Series[2].Start != "2026-03-01" {
		t.Errorf("serie mensual = %+v", b.Series)
	}
	if b.TopCards == nil || b.ByPayment == nil {
		t.Error("las listas vacías deben ir como [] y no null")
	}
}

func TestBreakdown_SinFechasSonLosUltimos30Dias(t *testing.T) {
	uc := NewBreakdownUseCase(&fakeDashboardRepo{})
	uc.now = func() time.Time { return at(19, 23) }
	b, err := uc.Execute(context.Background(), "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if b.From != "2026-09-20" || b.To != "2026-10-19" || len(b.Series) != 30 {
		t.Errorf("rango por defecto = %s..%s (%d días)", b.From, b.To, len(b.Series))
	}
}

func TestBreakdown_RangosInvalidos(t *testing.T) {
	uc := NewBreakdownUseCase(&fakeDashboardRepo{})
	for _, r := range [][2]string{
		{"2026-10-10", "2026-10-01"}, // invertido
		{"2026-10-01", ""},           // incompleto
		{"10/01/2026", "2026-10-02"}, // formato
		{"2020-01-01", "2026-01-01"}, // más de 2 años
	} {
		if _, err := uc.Execute(context.Background(), r[0], r[1], 0); !errors.Is(err, ErrInvalidRange) {
			t.Errorf("%v: err = %v, se esperaba ErrInvalidRange", r, err)
		}
	}
}

func TestSummary_InventarioDeSellado(t *testing.T) {
	repo := &fakeDashboardRepo{sealed: []dashboard.SealedProduct{
		{ID: 1, Title: "Agotado", Active: true, Stock: 2, Reserved: 2, PriceCOP: 1000},
		{ID: 2, Title: "Pocas", Active: true, Stock: 5, Reserved: 2, PriceCOP: 1000},
		{ID: 3, Title: "Muchas", Active: true, Stock: 10, PriceCOP: 500},
		{ID: 4, Title: "Oculto", Active: false, Stock: 7, PriceCOP: 9999},
	}}
	got, err := newTestUseCase(repo, testNow).Execute(context.Background(), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	s := got.Sealed
	if s.Products != 4 || s.Active != 3 || s.Hidden != 1 || s.StockUnits != 24 || s.ReservedUnits != 4 || s.AvailableUnits != 20 {
		t.Errorf("conteos = %+v", s)
	}
	// El valor solo cuenta lo disponible de los activos: 3×1000 + 10×500.
	if s.StockValueCOP != 8000 {
		t.Errorf("stock_value_cop = %d, se esperaba 8000", s.StockValueCOP)
	}
	if len(s.SoldOut) != 1 || s.SoldOut[0].ID != 1 || len(s.LowStock) != 1 || s.LowStock[0].Available != 3 {
		t.Errorf("sold_out = %+v, low_stock = %+v", s.SoldOut, s.LowStock)
	}
}
