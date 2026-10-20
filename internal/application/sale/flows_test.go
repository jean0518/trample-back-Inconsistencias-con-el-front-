package sale

import (
	"context"
	"errors"
	"testing"
	"time"

	"trample-back/internal/domain/auth"
	"trample-back/internal/domain/payment"
	"trample-back/internal/domain/sale"
	"trample-back/internal/ports/out"
)

// unverifiedUsers devuelve clientes que no han verificado su correo.
type unverifiedUsers struct{}

func (unverifiedUsers) FindByID(_ context.Context, id string) (auth.User, error) {
	return auth.User{ID: id, Email: "ana@example.com", EmailVerified: false}, nil
}

// ---------------------------------------------------------------------------
// Correo verificado para comprar
// ---------------------------------------------------------------------------

func TestCheckout_ExigeCorreoVerificado(t *testing.T) {
	uc, _, sales, _ := newCheckoutFixture()
	uc.WithEmailVerification(unverifiedUsers{})

	if _, err := uc.Execute(context.Background(), onlineInput(), []int64{1, 2}); !errors.Is(err, sale.ErrEmailNotVerified) {
		t.Fatalf("err = %v, se esperaba ErrEmailNotVerified", err)
	}
	if len(sales.checkouts) != 0 {
		t.Error("se abrió un checkout con el correo sin verificar")
	}

	// Con el correo verificado pasa.
	uc.WithEmailVerification(fakeUsers{})
	if _, err := uc.Execute(context.Background(), onlineInput(), []int64{1, 2}); err != nil {
		t.Fatalf("con correo verificado: %v", err)
	}
}

func TestRecogida_ExigeCorreoVerificadoSalvoAlStaff(t *testing.T) {
	uc, _, sales := newConfirm()
	uc.WithEmailVerification(unverifiedUsers{})

	if _, err := uc.Execute(context.Background(), storeInput(), []int64{1, 2}); !errors.Is(err, sale.ErrEmailNotVerified) {
		t.Fatalf("err = %v, se esperaba ErrEmailNotVerified", err)
	}
	if len(sales.sales) != 0 {
		t.Fatal("se apartó un pedido con el correo sin verificar")
	}

	// El staff registra ventas a nombre propio: no se le exige.
	staff := storeInput()
	staff.IsAdmin = true
	staff.PaymentMethod = sale.PaymentCash
	if _, err := uc.Execute(context.Background(), staff, []int64{1, 2}); err != nil {
		t.Fatalf("venta del staff: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Pago en proceso: "pedido recibido" y hasta 2 horas de espera
// ---------------------------------------------------------------------------

func TestPagoEnProceso_RecibidoUnaSolaVezYRetieneHasta2Horas(t *testing.T) {
	check, sales, gateway, ref := abrirCheckout(t, sale.PaymentBold)
	n, m := newNotifier()
	check.WithNotifier(n)
	gateway.status = payment.Status{Reference: ref, Status: payment.StatusPending}

	for range 3 { // el cliente refresca el carrito y la reconciliación consulta
		st, err := check.Execute(context.Background(), testUser, ref, false)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if st.Status != sale.CheckoutOpen || st.GatewayStatus != payment.StatusPending {
			t.Fatalf("estado = %q/%q, se esperaba open/pending", st.Status, st.GatewayStatus)
		}
	}
	waitFor(t, &m.received, 1)
	settle(t, &m.received, 1)
	if len(sales.sales) != 0 {
		t.Fatal("un pago en proceso no debe crear el pedido")
	}
	if c := sales.checkouts[ref]; c.ExpiresAt.Before(time.Now().Add(sale.PendingPaymentWindow - time.Minute)) {
		t.Errorf("la retención vence %v, se esperaba ~2 horas", time.Until(c.ExpiresAt))
	}

	// El pago se aprueba dentro de la ventana: llega la confirmación.
	gateway.status = payment.Status{Reference: ref, Status: payment.StatusApproved, PaymentID: "tx-1"}
	if _, err := check.Execute(context.Background(), testUser, ref, false); err != nil {
		t.Fatalf("aprobar: %v", err)
	}
	waitFor(t, &m.confirmed, 1)
}

// Durante la espera la reconciliación sigue preguntando por el pago aunque el
// checkout no haya vencido.
func TestPagoEnProceso_LaReconciliacionLoSigueConsultando(t *testing.T) {
	check, sales, gateway, ref := abrirCheckout(t, sale.PaymentBold)
	gateway.status = payment.Status{Reference: ref, Status: payment.StatusPending}
	if _, err := check.Execute(context.Background(), testUser, ref, false); err != nil {
		t.Fatal(err)
	}

	gateway.status = payment.Status{Reference: ref, Status: payment.StatusApproved, PaymentID: "tx-2"}
	got, err := NewReconcileCheckoutsUseCase(sales, check).Execute(context.Background())
	if err != nil || got != 1 {
		t.Fatalf("reconciliados = %d, %v; se esperaba 1", got, err)
	}
}

func TestExpireCheckouts_AvisaSoloALosPagosEnProceso(t *testing.T) {
	sales := newFakeSales()
	sales.expired = []sale.CheckoutIntent{
		{Reference: "abandonado", UserID: "u1"},
		{Reference: "pse", UserID: "u2", TotalCOP: 5000, PaymentPendingAt: time.Now().Add(-2 * time.Hour)},
	}
	n, m := newNotifier()
	if _, err := NewExpireCheckoutsUseCase(sales).WithNotifier(n).Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, &m.failed, 1)
	settle(t, &m.failed, 1)
	if !m.lastFailure.Expired || m.lastFailure.Reference != "pse" {
		t.Errorf("aviso inesperado: %+v", m.lastFailure)
	}
}

// ---------------------------------------------------------------------------
// Anulaciones
// ---------------------------------------------------------------------------

func TestAnulacionPorConsulta_AvisaAlClienteUnaVez(t *testing.T) {
	check, _, gateway, ref := abrirCheckout(t, sale.PaymentBold)
	n, m := newNotifier()
	check.WithNotifier(n)
	gateway.status = payment.Status{Reference: ref, Status: payment.StatusVoided}

	for range 2 {
		if _, err := check.Execute(context.Background(), testUser, ref, false); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, &m.failed, 1)
	settle(t, &m.failed, 1)
	if !m.lastFailure.Voided {
		t.Error("el aviso debía ser de pago anulado")
	}
}

func TestAnulacionDeUnPedidoCreado_AvisaALosAdmins(t *testing.T) {
	_, sales, gateway, ref := abrirCheckout(t, sale.PaymentBold)
	gateway.status = payment.Status{Reference: ref, Status: payment.StatusApproved, PaymentID: "tx-1"}
	n, m := newNotifier()
	uc := NewProcessPaymentEventUseCase(sales, gateway).WithNotifier(n)

	if _, err := uc.Execute(context.Background(), payment.Event{ID: "evt-a", Type: EventSaleApproved, Reference: ref}); err != nil {
		t.Fatal(err)
	}
	if _, err := uc.Execute(context.Background(), payment.Event{ID: "evt-v", Type: EventVoidApproved, Reference: ref}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, &m.alerts, 2)
	if m.lastAlert != out.AlertPaymentVoided {
		t.Errorf("alerta = %v, se esperaba AlertPaymentVoided", m.lastAlert)
	}
}

// ---------------------------------------------------------------------------
// Cancelación solicitada por el cliente
// ---------------------------------------------------------------------------

func TestCancelacionSolicitada_AprobarConMotivoDelCliente(t *testing.T) {
	uc, sales, mailer, id := pendingPickup(t, time.Now().Add(48*time.Hour))
	uc.notifier.WithAdmins(fakeAdmins{"a@trample.co"})

	if _, err := uc.RequestCancel(context.Background(), id, "otro"); !errors.Is(err, sale.ErrForbidden) {
		t.Fatalf("pedido ajeno: err = %v, se esperaba ErrForbidden", err)
	}
	s, err := uc.RequestCancel(context.Background(), id, testUser)
	if err != nil {
		t.Fatalf("RequestCancel: %v", err)
	}
	if s.Status != sale.StatusPendingPickup || len(sales.restocked) != 0 {
		t.Fatal("solicitar no debe cancelar ni devolver el stock")
	}
	waitFor(t, &mailer.cancelReq, 1)
	waitFor(t, &mailer.alerts, 1)
	if _, err := uc.RequestCancel(context.Background(), id, testUser); !errors.Is(err, sale.ErrCancelAlreadyRequested) {
		t.Fatalf("segunda solicitud: err = %v", err)
	}

	if _, err := uc.Cancel(context.Background(), id); err != nil {
		t.Fatalf("aprobar: %v", err)
	}
	waitFor(t, &mailer.cancelled, 1)
	if mailer.lastCancel != out.CancelRequested {
		t.Errorf("motivo = %v, se esperaba CancelRequested", mailer.lastCancel)
	}
}

func TestCancelacionSolicitada_Rechazar(t *testing.T) {
	uc, sales, mailer, id := pendingPickup(t, time.Now().Add(48*time.Hour))

	if _, err := uc.RejectCancel(context.Background(), id); !errors.Is(err, sale.ErrNoCancelRequest) {
		t.Fatalf("sin solicitud: err = %v, se esperaba ErrNoCancelRequest", err)
	}
	if _, err := uc.RequestCancel(context.Background(), id, testUser); err != nil {
		t.Fatal(err)
	}
	s, err := uc.RejectCancel(context.Background(), id)
	if err != nil {
		t.Fatalf("RejectCancel: %v", err)
	}
	if s.Status != sale.StatusPendingPickup || !sales.sales[id].CancelRequestedAt.IsZero() {
		t.Error("el pedido debe seguir apartado y sin solicitud")
	}
	waitFor(t, &mailer.cancelRej, 1)
}

func TestCancelarSinSolicitud_EsDecisionDeLaTienda(t *testing.T) {
	uc, _, mailer, id := pendingPickup(t, time.Now().Add(48*time.Hour))
	if _, err := uc.Cancel(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	waitFor(t, &mailer.cancelled, 1)
	if mailer.lastCancel != out.CancelByStore {
		t.Errorf("motivo = %v, se esperaba CancelByStore", mailer.lastCancel)
	}
}

// ---------------------------------------------------------------------------
// Recordatorio de recogida
// ---------------------------------------------------------------------------

func TestRecordatorio_SoloElDiaAnteriorYUnaVez(t *testing.T) {
	uc, sales, mailer, _ := pendingPickup(t, time.Now().Add(30*time.Hour)) // vence mañana
	sales.sales[8] = sale.Sale{ID: 8, UserID: testUser, Status: sale.StatusPendingPickup, PickupDeadline: time.Now().Add(60 * time.Hour)}

	for range 2 {
		if _, err := uc.SendReminders(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, &mailer.reminded, 1)
	settle(t, &mailer.reminded, 1)
}

// ---------------------------------------------------------------------------
// Revisión, envío y entrega
// ---------------------------------------------------------------------------

func newFulfillment() (*FulfillmentUseCase, *fakeSales, *fakeMailer) {
	sales := newFakeSales()
	sales.sales[5] = sale.Sale{ID: 5, UserID: testUser, Status: sale.StatusPaid, Fulfillment: sale.FulfillmentShipping}
	n, m := newNotifier()
	return NewFulfillmentUseCase(sales, n), sales, m
}

func TestEnvio_ExigeTransportadoraYAvisaEnvioYEntrega(t *testing.T) {
	uc, _, m := newFulfillment()

	if _, err := uc.MarkShipped(context.Background(), 5, "  ", "123"); !errors.Is(err, sale.ErrInvalidInput) {
		t.Fatalf("sin transportadora: err = %v", err)
	}
	if _, err := uc.MarkDelivered(context.Background(), 5); !errors.Is(err, sale.ErrNotShippable) {
		t.Fatalf("entregar sin enviar: err = %v", err)
	}
	s, err := uc.MarkShipped(context.Background(), 5, "Servientrega", " 9876 ")
	if err != nil {
		t.Fatalf("MarkShipped: %v", err)
	}
	if s.Carrier != "Servientrega" || s.TrackingNumber != "9876" {
		t.Errorf("despacho = %q/%q", s.Carrier, s.TrackingNumber)
	}
	waitFor(t, &m.shipped, 1)

	if _, err := uc.MarkDelivered(context.Background(), 5); err != nil {
		t.Fatalf("MarkDelivered: %v", err)
	}
	waitFor(t, &m.delivered, 1)
}

// Un pedido en revisión (p. ej. pago anulado o tardío) no se despacha hasta
// confirmarlo; al confirmarlo le llega la confirmación al cliente.
func TestRevision_BloqueaElEnvioHastaConfirmar(t *testing.T) {
	uc, sales, m := newFulfillment()
	s := sales.sales[5]
	s.RequiresReview = true
	sales.sales[5] = s

	if _, err := uc.MarkShipped(context.Background(), 5, "Servientrega", ""); !errors.Is(err, sale.ErrNotShippable) {
		t.Fatalf("enviar en revisión: err = %v, se esperaba ErrNotShippable", err)
	}
	if _, err := uc.ConfirmReview(context.Background(), 5); err != nil {
		t.Fatalf("ConfirmReview: %v", err)
	}
	waitFor(t, &m.confirmed, 1)
	if _, err := uc.ConfirmReview(context.Background(), 5); !errors.Is(err, sale.ErrNotInReview) {
		t.Fatalf("segunda confirmación: err = %v", err)
	}
	if _, err := uc.MarkShipped(context.Background(), 5, "Servientrega", ""); err != nil {
		t.Fatalf("enviar ya revisado: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Listado del panel
// ---------------------------------------------------------------------------

func TestSearch_ValidaYAcotaLosFiltros(t *testing.T) {
	sales := newFakeSales()
	uc := NewListSalesUseCase(sales)

	if _, _, err := uc.Search(context.Background(), sale.ListFilter{Status: "pagado"}); !errors.Is(err, sale.ErrInvalidInput) {
		t.Fatalf("estado desconocido: err = %v", err)
	}
	if _, _, err := uc.Search(context.Background(), sale.ListFilter{ShippingStatus: "lost"}); !errors.Is(err, sale.ErrInvalidInput) {
		t.Fatalf("shipping_status desconocido: err = %v", err)
	}

	if _, _, err := uc.Search(context.Background(), sale.ListFilter{Limit: 5000, Offset: -3}); err != nil {
		t.Fatal(err)
	}
	if sales.lastFilter.Limit != 500 || sales.lastFilter.Offset != 0 {
		t.Errorf("límites = %d/%d", sales.lastFilter.Limit, sales.lastFilter.Offset)
	}
	if _, _, err := uc.Search(context.Background(), sale.ListFilter{}); err != nil || sales.lastFilter.Limit != 50 {
		t.Errorf("límite por defecto = %d, %v", sales.lastFilter.Limit, err)
	}

	// Los conteos ignoran el estado: cada tarjeta es uno.
	if _, err := uc.Counts(context.Background(), sale.ListFilter{Status: sale.StatusPaid, Fulfillment: sale.FulfillmentShipping}); err != nil {
		t.Fatal(err)
	}
	if sales.lastFilter.Status != "" || sales.lastFilter.Fulfillment != sale.FulfillmentShipping {
		t.Errorf("filtro de conteos = %+v", sales.lastFilter)
	}
}
