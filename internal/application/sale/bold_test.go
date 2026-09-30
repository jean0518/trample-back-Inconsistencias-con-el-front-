package sale

import (
	"context"
	"errors"
	"testing"

	"trample-back/internal/domain/payment"
	"trample-back/internal/domain/reservation"
	"trample-back/internal/domain/sale"
)

// abrirCheckout deja un checkout listo y devuelve el caso de uso de consulta con
// la pasarela programable.
func abrirCheckout(t *testing.T, method string) (*CheckPaymentStatusUseCase, *fakeSales, *fakeGateway, string) {
	t.Helper()
	_, _, sales, gateway := newCheckoutFixture()

	result, err := NewCreateCheckoutUseCase(
		&fakeReservations{list: []reservation.Reservation{cartReservation(1, testUser, 45000, 11.25, 1)}},
		sales, gateway, 30, testFront,
	).Execute(context.Background(), sale.ConfirmInput{
		UserID:        testUser,
		Fulfillment:   sale.FulfillmentPickup,
		PaymentMethod: sale.PaymentBold,
	}, []int64{1})
	if err != nil {
		t.Fatalf("preparar checkout: %v", err)
	}
	return NewCheckPaymentStatusUseCase(sales, gateway), sales, gateway, result.Checkout.Reference
}

func TestCheckPaymentStatus_PagoAprobadoCreaElPedido(t *testing.T) {
	uc, sales, gateway, ref := abrirCheckout(t, sale.PaymentBold)
	gateway.status = payment.Status{Reference: ref, Status: payment.StatusApproved, PaymentID: "tx-1"}

	status, err := uc.Execute(context.Background(), testUser, ref, false)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if status.Status != sale.CheckoutPaid {
		t.Errorf("status = %q, se esperaba %q", status.Status, sale.CheckoutPaid)
	}
	if status.Sale == nil {
		t.Fatal("no se devolvió el pedido aunque el pago estaba aprobado")
	}
	if len(sales.sales) != 1 {
		t.Fatalf("ventas creadas = %d, se esperaba 1", len(sales.sales))
	}
	if sales.sales[status.Sale.ID].PaymentReference != ref {
		t.Errorf("el pedido no guarda la referencia de pago %q", ref)
	}
}

// Idempotencia: Bold reintenta el webhook y el frontend puede preguntar varias
// veces. Nunca debe existir más de un pedido por pago.
func TestCheckPaymentStatus_NoDuplicaElPedido(t *testing.T) {
	uc, sales, gateway, ref := abrirCheckout(t, sale.PaymentBold)
	gateway.status = payment.Status{Reference: ref, Status: payment.StatusApproved, PaymentID: "tx-1"}

	var first sale.PaymentStatus
	for i := range 3 {
		status, err := uc.Execute(context.Background(), testUser, ref, false)
		if err != nil {
			t.Fatalf("consulta %d: %v", i+1, err)
		}
		if i == 0 {
			first = status
			continue
		}
		if status.Sale == nil || status.Sale.ID != first.Sale.ID {
			t.Fatalf("consulta %d devolvió un pedido distinto: %+v", i+1, status.Sale)
		}
	}
	if len(sales.sales) != 1 {
		t.Fatalf("ventas creadas = %d, se esperaba exactamente 1", len(sales.sales))
	}
}

func TestCheckPaymentStatus_PagoEnCursoNoCreaPedido(t *testing.T) {
	uc, sales, gateway, ref := abrirCheckout(t, sale.PaymentBold)
	gateway.status = payment.Status{Reference: ref, Status: payment.StatusPending}

	status, err := uc.Execute(context.Background(), testUser, ref, false)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if status.Status != sale.CheckoutOpen {
		t.Errorf("status = %q, se esperaba %q", status.Status, sale.CheckoutOpen)
	}
	if status.Sale != nil {
		t.Error("se devolvió un pedido mientras el cliente seguía pagando")
	}
	if len(sales.sales) != 0 {
		t.Fatal("se creó un pedido sin pago confirmado")
	}
}

func TestCheckPaymentStatus_PagoRechazadoNoCreaPedido(t *testing.T) {
	uc, sales, gateway, ref := abrirCheckout(t, sale.PaymentBold)
	gateway.status = payment.Status{Reference: ref, Status: payment.StatusRejected}

	status, err := uc.Execute(context.Background(), testUser, ref, false)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if status.Status != sale.CheckoutFailed {
		t.Errorf("status = %q, se esperaba %q", status.Status, sale.CheckoutFailed)
	}
	if len(sales.sales) != 0 {
		t.Fatal("un pago rechazado no debe crear ningún pedido")
	}
}

func TestCheckPaymentStatus_PasarelaCaida(t *testing.T) {
	uc, sales, gateway, ref := abrirCheckout(t, sale.PaymentBold)
	// La pasarela no responde; el webhook puede llegar igual más tarde.
	gateway.statusErr = errors.New("timeout")

	status, err := uc.Execute(context.Background(), testUser, ref, false)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if status.Status != sale.CheckoutOpen {
		t.Errorf("status = %q, se esperaba %q", status.Status, sale.CheckoutOpen)
	}
	if len(sales.sales) != 0 {
		t.Fatal("se creó un pedido sin saber el estado del pago")
	}
}

func TestCheckPaymentStatus_OtroUsuario(t *testing.T) {
	uc, _, _, ref := abrirCheckout(t, sale.PaymentBold)

	if _, err := uc.Execute(context.Background(), "99", ref, false); !errors.Is(err, sale.ErrForbidden) {
		t.Fatalf("err = %v, se esperaba ErrForbidden", err)
	}
}

func TestCheckPaymentStatus_ReferenciaDesconocida(t *testing.T) {
	uc, _, _, _ := abrirCheckout(t, sale.PaymentBold)

	if _, err := uc.Execute(context.Background(), testUser, "TRM-no-existe", false); !errors.Is(err, sale.ErrPaymentNotFound) {
		t.Fatalf("err = %v, se esperaba ErrPaymentNotFound", err)
	}
	if _, err := uc.Execute(context.Background(), testUser, "", false); !errors.Is(err, sale.ErrPaymentNotFound) {
		t.Fatalf("err = %v, se esperaba ErrPaymentNotFound", err)
	}
}

// El webhook es la vía principal en producción: si trae SALE_APPROVED, el pedido
// se crea sin que el frontend pregunte nada.
func TestProcessPaymentEvent_AprobadoCreaElPedido(t *testing.T) {
	_, sales, _, ref := abrirCheckout(t, sale.PaymentBold)
	uc := NewProcessPaymentEventUseCase(sales)

	processed, err := uc.Execute(context.Background(), payment.Event{
		ID: "evt-1", Type: EventSaleApproved, Reference: ref, PaymentID: "tx-1",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !processed {
		t.Error("el evento no se procesó")
	}
	if len(sales.sales) != 1 {
		t.Fatalf("ventas creadas = %d, se esperaba 1", len(sales.sales))
	}
}

// Bold reintenta el mismo evento hasta 5 veces: solo el primero debe hacer algo.
func TestProcessPaymentEvent_Idempotente(t *testing.T) {
	_, sales, _, ref := abrirCheckout(t, sale.PaymentBold)
	uc := NewProcessPaymentEventUseCase(sales)

	evt := payment.Event{ID: "evt-1", Type: EventSaleApproved, Reference: ref, PaymentID: "tx-1"}
	for i := range 3 {
		processed, err := uc.Execute(context.Background(), evt)
		if err != nil {
			t.Fatalf("intento %d: %v", i+1, err)
		}
		if i == 0 && !processed {
			t.Error("el primer intento debería procesarse")
		}
		if i > 0 && processed {
			t.Error("un evento repetido no debe procesarse de nuevo")
		}
	}
	if len(sales.sales) != 1 {
		t.Fatalf("ventas creadas = %d, se esperaba 1", len(sales.sales))
	}
}

func TestProcessPaymentEvent_Rechazado(t *testing.T) {
	_, sales, _, ref := abrirCheckout(t, sale.PaymentBold)
	uc := NewProcessPaymentEventUseCase(sales)

	if _, err := uc.Execute(context.Background(), payment.Event{
		ID: "evt-2", Type: EventSaleRejected, Reference: ref,
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if sales.checkouts[ref].Status != sale.CheckoutFailed {
		t.Errorf("status del checkout = %q, se esperaba %q", sales.checkouts[ref].Status, sale.CheckoutFailed)
	}
	if len(sales.sales) != 0 {
		t.Fatal("un pago rechazado no debe crear ningún pedido")
	}
}

// Un pago hecho fuera de la tienda llega sin referencia: se registra y se
// responde 200, sin tocar nada.
func TestProcessPaymentEvent_SinReferencia(t *testing.T) {
	_, sales, _, _ := abrirCheckout(t, sale.PaymentBold)
	uc := NewProcessPaymentEventUseCase(sales)

	processed, err := uc.Execute(context.Background(), payment.Event{ID: "evt-3", Type: EventSaleApproved})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if processed {
		t.Error("un evento sin referencia no debe considerarse procesado")
	}
	if len(sales.sales) != 0 {
		t.Fatal("se creó un pedido desde un evento sin referencia")
	}
}

func TestExpireCheckouts(t *testing.T) {
	_, sales, _, _ := abrirCheckout(t, sale.PaymentBold)
	sales.expired = 3

	n, err := NewExpireCheckoutsUseCase(sales).Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if n != 3 {
		t.Errorf("cerrados = %d, se esperaban 3", n)
	}
}
