package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"trample-back/internal/domain/sale"
)

// El frontend distingue "verifica tu correo" por el código, no por el texto.
func TestRespondSaleError_CorreoSinVerificar(t *testing.T) {
	w := httptest.NewRecorder()
	respondSaleError(w, fmt.Errorf("checkout: %w", sale.ErrEmailNotVerified))

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, se esperaba 403", w.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != "email_not_verified" || body["error"] == "" {
		t.Errorf("body = %v", body)
	}
}

func TestRespondSaleError_EstadosDelPedidoSonConflicto(t *testing.T) {
	for _, err := range []error{sale.ErrCancelAlreadyRequested, sale.ErrNoCancelRequest, sale.ErrNotShippable, sale.ErrNotInReview} {
		w := httptest.NewRecorder()
		respondSaleError(w, err)
		if w.Code != http.StatusConflict {
			t.Errorf("%v: status = %d, se esperaba 409", err, w.Code)
		}
	}
}

func TestShippingStatusSoloEnPedidosPagadosConEnvio(t *testing.T) {
	now := time.Now()
	cases := []struct {
		s    sale.Sale
		want string
	}{
		{sale.Sale{Fulfillment: sale.FulfillmentPickup, Status: sale.StatusPaid}, ""},
		{sale.Sale{Fulfillment: sale.FulfillmentShipping, Status: sale.StatusPendingPickup}, ""},
		{sale.Sale{Fulfillment: sale.FulfillmentShipping, Status: sale.StatusPaid}, "pending"},
		{sale.Sale{Fulfillment: sale.FulfillmentShipping, Status: sale.StatusPaid, ShippedAt: now}, "shipped"},
		{sale.Sale{Fulfillment: sale.FulfillmentShipping, Status: sale.StatusPaid, ShippedAt: now, DeliveredAt: now}, "delivered"},
	}
	for _, c := range cases {
		if got := shippingStatus(c.s); got != c.want {
			t.Errorf("shippingStatus(%+v) = %q, se esperaba %q", c.s, got, c.want)
		}
	}
}
