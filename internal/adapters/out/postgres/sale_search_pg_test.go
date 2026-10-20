package postgres

import (
	"strings"
	"testing"
	"time"

	"trample-back/internal/domain/sale"
)

func TestSaleWhere_SinFiltros(t *testing.T) {
	where, args := saleWhere(sale.ListFilter{}, true)
	if where != "" || len(args) != 0 {
		t.Errorf("where = %q, args = %v", where, args)
	}
}

func TestSaleWhere_TodosLosFiltrosVanComoParametros(t *testing.T) {
	from := time.Date(2026, 10, 1, 5, 0, 0, 0, time.UTC)
	where, args := saleWhere(sale.ListFilter{
		From: from, To: from.AddDate(0, 0, 7),
		Status: sale.StatusPaid, ShippingStatus: sale.ShippingShipped,
		Fulfillment: sale.FulfillmentShipping, PaymentMethod: sale.PaymentBold,
		Query: "#128",
	}, true)

	for _, want := range []string{
		"s.created_at >= $1", "s.created_at < $2", "s.status = $3", "s.shipped_at IS NOT NULL",
		"s.fulfillment = $4", "s.payment_method = $5", "ILIKE $6", "s.id = $7",
	} {
		if !strings.Contains(where, want) {
			t.Errorf("falta %q en %s", want, where)
		}
	}
	if len(args) != 7 || args[5] != "%#128%" || args[6] != int64(128) {
		t.Errorf("args = %v", args)
	}
	// Nada del texto buscado se pega al SQL.
	if strings.Contains(where, "128") {
		t.Error("el texto buscado no debe ir dentro del SQL")
	}
}

func TestSaleWhere_ConteosIgnoranElEstado(t *testing.T) {
	where, _ := saleWhere(sale.ListFilter{Status: sale.StatusPaid}, false)
	if where != "" {
		t.Errorf("where = %q", where)
	}
}

func TestSaleWhere_EscapaComodines(t *testing.T) {
	_, args := saleWhere(sale.ListFilter{Query: "100%_x"}, true)
	if args[0] != `%100\%\_x%` {
		t.Errorf("args = %v", args)
	}
}
