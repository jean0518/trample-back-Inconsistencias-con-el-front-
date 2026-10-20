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
		Fulfillment:   sale.FulfillmentShipping,
		Address:       "Calle 1 # 2-3",
		City:          "Cali",
		Phone:         "3001234567",
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

// Un intento rechazado no cierra el checkout: el cliente puede reintentar con
// otra tarjeta en el mismo pago, y el stock debe seguir retenido.
func TestCheckPaymentStatus_PagoRechazadoDejaReintentar(t *testing.T) {
	uc, sales, gateway, ref := abrirCheckout(t, sale.PaymentBold)
	gateway.status = payment.Status{Reference: ref, Status: payment.StatusRejected}

	status, err := uc.Execute(context.Background(), testUser, ref, false)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if status.Status != sale.CheckoutOpen {
		t.Errorf("status = %q, el checkout debía seguir %q para poder reintentar", status.Status, sale.CheckoutOpen)
	}
	if status.GatewayStatus != payment.StatusRejected {
		t.Errorf("gateway_status = %q, el frontend necesita saber que el intento falló", status.GatewayStatus)
	}
	if len(sales.rejectRefs) != 0 {
		t.Error("un intento rechazado liberó el stock")
	}
	if len(sales.sales) != 0 {
		t.Fatal("un pago rechazado no debe crear ningún pedido")
	}
}

// El reintento aprobado después de un rechazo debe terminar en pedido normal, sin
// revisión manual: el checkout nunca se cerró.
func TestCheckPaymentStatus_RechazoYLuegoAprobadoCreaPedidoSinRevision(t *testing.T) {
	uc, sales, gateway, ref := abrirCheckout(t, sale.PaymentBold)

	gateway.status = payment.Status{Reference: ref, Status: payment.StatusRejected}
	if _, err := uc.Execute(context.Background(), testUser, ref, false); err != nil {
		t.Fatalf("primer intento: %v", err)
	}

	gateway.status = payment.Status{Reference: ref, Status: payment.StatusApproved, PaymentID: "tx-2", AmountCOP: 70000}
	status, err := uc.Execute(context.Background(), testUser, ref, false)
	if err != nil {
		t.Fatalf("reintento: %v", err)
	}
	if status.Sale == nil {
		t.Fatal("el pago aprobado tras un rechazo no creó el pedido")
	}
	if status.Sale.RequiresReview {
		t.Error("un reintento legítimo no debe requerir revisión manual")
	}
	if len(sales.sales) != 1 {
		t.Fatalf("ventas = %d, se esperaba 1", len(sales.sales))
	}
}

// Una transacción anulada deja de ser un pago válido: el checkout se cierra.
func TestCheckPaymentStatus_PagoAnuladoCierraElCheckout(t *testing.T) {
	uc, sales, gateway, ref := abrirCheckout(t, sale.PaymentBold)
	gateway.status = payment.Status{Reference: ref, Status: payment.StatusVoided}

	status, err := uc.Execute(context.Background(), testUser, ref, false)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if status.Status != sale.CheckoutFailed {
		t.Errorf("status = %q, se esperaba %q", status.Status, sale.CheckoutFailed)
	}
	if len(sales.sales) != 0 {
		t.Fatal("un pago anulado no debe crear ningún pedido")
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
	_, sales, gateway, ref := abrirCheckout(t, sale.PaymentBold)
	gateway.status = payment.Status{Reference: ref, Status: payment.StatusApproved, PaymentID: "tx-1"}
	uc := NewProcessPaymentEventUseCase(sales, gateway)

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
	_, sales, gateway, ref := abrirCheckout(t, sale.PaymentBold)
	gateway.status = payment.Status{Reference: ref, Status: payment.StatusApproved, PaymentID: "tx-1"}
	uc := NewProcessPaymentEventUseCase(sales, gateway)

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

func TestProcessPaymentEvent_RechazadoNoCierraElCheckout(t *testing.T) {
	_, sales, gateway, ref := abrirCheckout(t, sale.PaymentBold)
	uc := NewProcessPaymentEventUseCase(sales, gateway)

	if _, err := uc.Execute(context.Background(), payment.Event{
		ID: "evt-2", Type: EventSaleRejected, Reference: ref,
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if sales.checkouts[ref].Status != sale.CheckoutOpen {
		t.Errorf("status del checkout = %q, debía seguir abierto para reintentar", sales.checkouts[ref].Status)
	}
	if len(sales.rejectRefs) != 0 {
		t.Error("un intento rechazado liberó el stock")
	}
	if len(sales.sales) != 0 {
		t.Fatal("un pago rechazado no debe crear ningún pedido")
	}
}

// Un pago hecho fuera de la tienda llega sin referencia: se registra y se
// responde 200, sin tocar nada.
func TestProcessPaymentEvent_SinReferencia(t *testing.T) {
	_, sales, gateway, _ := abrirCheckout(t, sale.PaymentBold)
	uc := NewProcessPaymentEventUseCase(sales, gateway)

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
	sales.expired = make([]sale.CheckoutIntent, 3)

	n, err := NewExpireCheckoutsUseCase(sales).Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if n != 3 {
		t.Errorf("cerrados = %d, se esperaban 3", n)
	}
}

// Un SALE_APPROVED falsificado (en pruebas la firma usa llave vacía, así que
// cualquiera puede generarla) no debe crear un pedido: la pasarela no lo confirma.
func TestProcessPaymentEvent_AprobadoFalsoNoCreaPedido(t *testing.T) {
	_, sales, gateway, ref := abrirCheckout(t, sale.PaymentBold)
	gateway.status = payment.Status{Reference: ref, Status: payment.StatusNotFound}
	uc := NewProcessPaymentEventUseCase(sales, gateway)

	_, err := uc.Execute(context.Background(), payment.Event{ID: "evt-fake", Type: EventSaleApproved, Reference: ref})
	if !errors.Is(err, payment.ErrNotConfirmed) {
		t.Fatalf("err = %v, se esperaba ErrNotConfirmed", err)
	}
	if len(sales.sales) != 0 {
		t.Fatal("se creó un pedido a partir de un evento que la pasarela no confirma")
	}
}

// Si procesar el evento falla, el reintento de Bold debe procesarse: el evento no
// puede quedar registrado, o se descartaría como duplicado y el pago quedaría sin
// pedido.
func TestProcessPaymentEvent_UnFalloNoImpideElReintento(t *testing.T) {
	_, sales, gateway, ref := abrirCheckout(t, sale.PaymentBold)
	gateway.status = payment.Status{Reference: ref, Status: payment.StatusApproved, PaymentID: "tx-1"}
	uc := NewProcessPaymentEventUseCase(sales, gateway)
	evt := payment.Event{ID: "evt-retry", Type: EventSaleApproved, Reference: ref, PaymentID: "tx-1"}

	sales.approveErr = errors.New("base de datos caída")
	if _, err := uc.Execute(context.Background(), evt); err == nil {
		t.Fatal("se esperaba el error del fallo para que el handler responda 500")
	}
	if len(sales.forgotten) != 1 || sales.forgotten[0] != "evt-retry" {
		t.Fatalf("el evento fallido no se olvidó: %v", sales.forgotten)
	}

	// Bold reintenta y esta vez la base de datos responde.
	sales.approveErr = nil
	processed, err := uc.Execute(context.Background(), evt)
	if err != nil {
		t.Fatalf("reintento: %v", err)
	}
	if !processed {
		t.Error("el reintento se descartó como duplicado")
	}
	if len(sales.sales) != 1 {
		t.Fatalf("ventas creadas = %d, se esperaba 1", len(sales.sales))
	}
}

func TestProcessPaymentEvent_PasarelaCaidaPermiteReintento(t *testing.T) {
	_, sales, gateway, ref := abrirCheckout(t, sale.PaymentBold)
	gateway.statusErr = errors.New("timeout")
	uc := NewProcessPaymentEventUseCase(sales, gateway)

	if _, err := uc.Execute(context.Background(), payment.Event{ID: "evt-gw", Type: EventSaleApproved, Reference: ref}); err == nil {
		t.Fatal("se esperaba error para que Bold reintente")
	}
	if len(sales.forgotten) != 1 {
		t.Error("el evento no se olvidó y el reintento se descartaría")
	}
}

// El monto que Bold cobró sale de la consulta a la pasarela: si no coincide con
// el pedido, se crea igual (el cliente pagó) pero se revisa a mano.
func TestProcessPaymentEvent_MontoDistintoRequiereRevision(t *testing.T) {
	_, sales, gateway, ref := abrirCheckout(t, sale.PaymentBold)
	gateway.status = payment.Status{Reference: ref, Status: payment.StatusApproved, PaymentID: "tx-1", AmountCOP: 1000}
	uc := NewProcessPaymentEventUseCase(sales, gateway)

	if _, err := uc.Execute(context.Background(), payment.Event{ID: "evt-amt", Type: EventSaleApproved, Reference: ref}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var created sale.Sale
	for _, s := range sales.sales {
		created = s
	}
	if !created.RequiresReview {
		t.Error("un pago por un monto distinto al del pedido debe marcarse para revisión")
	}
}

func TestProcessPaymentEvent_MontoCorrectoNoRequiereRevision(t *testing.T) {
	_, sales, gateway, ref := abrirCheckout(t, sale.PaymentBold)
	gateway.status = payment.Status{Reference: ref, Status: payment.StatusApproved, PaymentID: "tx-1", AmountCOP: 70000}
	uc := NewProcessPaymentEventUseCase(sales, gateway)

	if _, err := uc.Execute(context.Background(), payment.Event{ID: "evt-ok", Type: EventSaleApproved, Reference: ref}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, s := range sales.sales {
		if s.RequiresReview {
			t.Error("un pago normal por el monto correcto no debe requerir revisión")
		}
	}
}

// VOID_REJECTED significa que la anulación falló: el pago sigue vigente y el
// checkout no debe cerrarse ni liberar el stock.
func TestProcessPaymentEvent_AnulacionRechazadaNoTocaNada(t *testing.T) {
	_, sales, gateway, ref := abrirCheckout(t, sale.PaymentBold)
	uc := NewProcessPaymentEventUseCase(sales, gateway)

	if _, err := uc.Execute(context.Background(), payment.Event{ID: "evt-vr", Type: EventVoidRejected, Reference: ref}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if sales.checkouts[ref].Status != sale.CheckoutOpen {
		t.Errorf("el checkout quedó %q, debía seguir abierto", sales.checkouts[ref].Status)
	}
	if len(sales.rejectRefs) != 0 {
		t.Error("una anulación rechazada liberó el stock")
	}
}

// Anular un pago ya aprobado deja un pedido que no debe despacharse: se marca
// para revisión.
func TestProcessPaymentEvent_AnulacionMarcaElPedidoParaRevision(t *testing.T) {
	_, sales, gateway, ref := abrirCheckout(t, sale.PaymentBold)
	gateway.status = payment.Status{Reference: ref, Status: payment.StatusApproved, PaymentID: "tx-1"}
	uc := NewProcessPaymentEventUseCase(sales, gateway)

	if _, err := uc.Execute(context.Background(), payment.Event{ID: "evt-a", Type: EventSaleApproved, Reference: ref}); err != nil {
		t.Fatalf("aprobar: %v", err)
	}
	if _, err := uc.Execute(context.Background(), payment.Event{ID: "evt-v", Type: EventVoidApproved, Reference: ref}); err != nil {
		t.Fatalf("anular: %v", err)
	}
	if len(sales.flagged) != 1 || sales.flagged[0] != ref {
		t.Fatalf("el pedido anulado no se marcó para revisión: %v", sales.flagged)
	}
}

// Un pago aprobado en la pasarela pero que el backend nunca registró (webhook
// perdido) se recupera aunque el cliente no vuelva al carrito.
func TestReconcile_RecuperaUnPagoPerdido(t *testing.T) {
	check, sales, gateway, ref := abrirCheckout(t, sale.PaymentBold)
	gateway.status = payment.Status{Reference: ref, Status: payment.StatusApproved, PaymentID: "tx-9"}
	// El checkout venció y se cerró antes de que se supiera del pago.
	c := sales.checkouts[ref]
	c.Status = sale.CheckoutExpired
	sales.checkouts[ref] = c

	n, err := NewReconcileCheckoutsUseCase(sales, check).Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if n != 1 {
		t.Fatalf("recuperados = %d, se esperaba 1", n)
	}
	if len(sales.sales) != 1 {
		t.Fatalf("ventas creadas = %d, se esperaba 1", len(sales.sales))
	}
	for _, s := range sales.sales {
		if !s.RequiresReview {
			t.Error("un pago recuperado de un checkout ya cerrado debe revisarse a mano")
		}
	}
}

func TestReconcile_PagoPendienteNoCreaPedido(t *testing.T) {
	check, sales, gateway, ref := abrirCheckout(t, sale.PaymentBold)
	gateway.status = payment.Status{Reference: ref, Status: payment.StatusNotFound}
	c := sales.checkouts[ref]
	c.Status = sale.CheckoutExpired
	sales.checkouts[ref] = c

	n, err := NewReconcileCheckoutsUseCase(sales, check).Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if n != 0 || len(sales.sales) != 0 {
		t.Fatalf("recuperados = %d, ventas = %d; no había ningún pago", n, len(sales.sales))
	}
}

// Es idempotente: reconciliar varias veces el mismo pago no duplica el pedido.
func TestReconcile_NoDuplicaElPedido(t *testing.T) {
	check, sales, gateway, ref := abrirCheckout(t, sale.PaymentBold)
	gateway.status = payment.Status{Reference: ref, Status: payment.StatusApproved, PaymentID: "tx-9"}
	c := sales.checkouts[ref]
	c.Status = sale.CheckoutExpired
	sales.checkouts[ref] = c

	uc := NewReconcileCheckoutsUseCase(sales, check)
	for i := range 3 {
		if _, err := uc.Execute(context.Background()); err != nil {
			t.Fatalf("pasada %d: %v", i+1, err)
		}
	}
	if len(sales.sales) != 1 {
		t.Fatalf("ventas creadas = %d, se esperaba exactamente 1", len(sales.sales))
	}
}

// El barrido espera un margen antes de soltar el stock de un pago en curso.
func TestExpireCheckouts_EsperaElMargenDeGracia(t *testing.T) {
	_, sales, _, _ := abrirCheckout(t, sale.PaymentBold)

	if _, err := NewExpireCheckoutsUseCase(sales).Execute(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if sales.expireGrace != paymentGrace || sales.expireGrace <= 0 {
		t.Errorf("margen = %v, se esperaba %v", sales.expireGrace, paymentGrace)
	}
}
