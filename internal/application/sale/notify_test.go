package sale

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"trample-back/internal/domain/auth"
	"trample-back/internal/domain/sale"
	"trample-back/internal/ports/out"
)

type fakeUsers struct{}

func (fakeUsers) FindByID(_ context.Context, id string) (auth.User, error) {
	return auth.User{ID: id, Email: "ana@example.com", FirstName: "Ana", EmailVerified: true}, nil
}

// fakeAdmins devuelve una lista fija de correos de administradores.
type fakeAdmins []string

func (a fakeAdmins) ListAdminEmails(context.Context) ([]string, error) { return a, nil }

// fakeMailer cuenta los correos por tipo y guarda el último motivo recibido.
type fakeMailer struct {
	confirmed, received, reserved, reminded, cancelled atomic.Int32
	cancelReq, cancelRej, shipped, delivered, failed   atomic.Int32
	admin, alerts                                      atomic.Int32

	mu             sync.Mutex
	lastReceived   out.ReceivedReason
	lastCancel     out.CancelReason
	lastAlert      out.AdminAlert
	lastFailure    out.PaymentFailure
	adminRecipient []string
}

func (m *fakeMailer) SendOrderConfirmed(context.Context, string, string, sale.Sale) error {
	m.confirmed.Add(1)
	return nil
}
func (m *fakeMailer) SendOrderReceived(_ context.Context, _, _ string, _ sale.Sale, r out.ReceivedReason) error {
	m.mu.Lock()
	m.lastReceived = r
	m.mu.Unlock()
	m.received.Add(1)
	return nil
}
func (m *fakeMailer) SendPickupReserved(context.Context, string, string, sale.Sale) error {
	m.reserved.Add(1)
	return nil
}
func (m *fakeMailer) SendPickupReminder(context.Context, string, string, sale.Sale) error {
	m.reminded.Add(1)
	return nil
}
func (m *fakeMailer) SendPickupCancelled(_ context.Context, _, _ string, _ sale.Sale, r out.CancelReason) error {
	m.mu.Lock()
	m.lastCancel = r
	m.mu.Unlock()
	m.cancelled.Add(1)
	return nil
}
func (m *fakeMailer) SendCancelRequested(context.Context, string, string, sale.Sale) error {
	m.cancelReq.Add(1)
	return nil
}
func (m *fakeMailer) SendCancelRejected(context.Context, string, string, sale.Sale) error {
	m.cancelRej.Add(1)
	return nil
}
func (m *fakeMailer) SendOrderShipped(context.Context, string, string, sale.Sale) error {
	m.shipped.Add(1)
	return nil
}
func (m *fakeMailer) SendOrderDelivered(context.Context, string, string, sale.Sale) error {
	m.delivered.Add(1)
	return nil
}
func (m *fakeMailer) SendPaymentFailed(_ context.Context, _, _ string, f out.PaymentFailure) error {
	m.mu.Lock()
	m.lastFailure = f
	m.mu.Unlock()
	m.failed.Add(1)
	return nil
}
func (m *fakeMailer) SendAdminNewOrder(_ context.Context, to string, _ out.OrderBuyer, _ sale.Sale) error {
	m.mu.Lock()
	m.adminRecipient = append(m.adminRecipient, to)
	m.mu.Unlock()
	m.admin.Add(1)
	return nil
}
func (m *fakeMailer) SendAdminAlert(_ context.Context, _ string, _ out.OrderBuyer, _ sale.Sale, a out.AdminAlert) error {
	m.mu.Lock()
	m.lastAlert = a
	m.mu.Unlock()
	m.alerts.Add(1)
	return nil
}

func waitFor(t *testing.T, c *atomic.Int32, want int32) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if c.Load() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("correos enviados = %d, se esperaban %d", c.Load(), want)
}

// settle espera a que terminen los envíos en segundo plano y comprueba que el
// contador no pasó de `want`.
func settle(t *testing.T, c *atomic.Int32, want int32) {
	t.Helper()
	time.Sleep(50 * time.Millisecond)
	if got := c.Load(); got != want {
		t.Fatalf("correos enviados = %d, se esperaban %d", got, want)
	}
}

func newNotifier() (*PurchaseNotifier, *fakeMailer) {
	m := &fakeMailer{}
	return NewPurchaseNotifier(fakeUsers{}, m).WithAdmins(fakeAdmins{"a@trample.co", "b@trample.co"}), m
}

func TestOrderConfirmedSoloAvisaCuandoElPedidoSeAcabaDeCrear(t *testing.T) {
	n, m := newNotifier()

	n.OrderConfirmed(sale.Sale{UserID: "u1", PaymentMethod: sale.PaymentBold, JustCreated: false}) // webhook repetido
	n.OrderConfirmed(sale.Sale{UserID: "u1", PaymentMethod: sale.PaymentBold, JustCreated: true})
	waitFor(t, &m.confirmed, 1)
	waitFor(t, &m.admin, 2) // un correo por admin
	settle(t, &m.confirmed, 1)
	settle(t, &m.admin, 2)
}

func TestNotificadorNilNoHaceNada(t *testing.T) {
	var n *PurchaseNotifier
	n.OrderConfirmed(sale.Sale{JustCreated: true})
	n.PaymentFailed("u1", out.PaymentFailure{})
	n.PaymentPending(sale.CheckoutIntent{})
	n.CancelRequested(sale.Sale{})
	n.PaymentVoided(sale.Sale{})
	n.AdminNewOrder(sale.Sale{})
}

func TestPaymentFailedEnviaCorreo(t *testing.T) {
	n, m := newNotifier()
	n.PaymentFailed("u1", out.PaymentFailure{Reference: "R1", TotalCOP: 1000})
	waitFor(t, &m.failed, 1)
}

// Un pago tardío no se confirma todavía: el cliente recibe "pedido recibido" y
// los admins el aviso del pedido (que llega marcado [REVISAR]).
func TestPagoTardioSeRecibeSinConfirmar(t *testing.T) {
	n, m := newNotifier()
	n.OrderConfirmed(sale.Sale{UserID: "u1", PaymentMethod: sale.PaymentBold, RequiresReview: true, JustCreated: true})
	waitFor(t, &m.received, 1)
	waitFor(t, &m.admin, 2)
	settle(t, &m.confirmed, 0)
	if m.lastReceived != out.ReceivedInReview {
		t.Errorf("motivo = %v, se esperaba ReceivedInReview", m.lastReceived)
	}

	n.ReviewConfirmed(sale.Sale{UserID: "u1"})
	waitFor(t, &m.confirmed, 1)
}

func TestPagoEnProcesoAvisaPedidoRecibido(t *testing.T) {
	n, m := newNotifier()
	n.PaymentPending(sale.CheckoutIntent{UserID: "u1", Reference: "TRM-1"})
	waitFor(t, &m.received, 1)
	if m.lastReceived != out.ReceivedPaymentPending {
		t.Errorf("motivo = %v, se esperaba ReceivedPaymentPending", m.lastReceived)
	}
	settle(t, &m.admin, 0) // todavía no hay pedido: los admins no se enteran
}

func TestAvisoALosAdminsEnCadaPedidoNuevo(t *testing.T) {
	n, m := newNotifier()

	n.PickupReserved(sale.Sale{UserID: "u1", PaymentMethod: sale.PaymentStore, Status: sale.StatusPendingPickup})
	waitFor(t, &m.reserved, 1)
	waitFor(t, &m.admin, 2)

	// La recogida pagada en el local ya se avisó al apartarse: solo va al cliente.
	n.OrderConfirmed(sale.Sale{UserID: "u1", PaymentMethod: sale.PaymentStore, JustCreated: true})
	waitFor(t, &m.confirmed, 1)
	settle(t, &m.admin, 2)

	// Venta en efectivo registrada por el staff: solo a los admins.
	n.AdminNewOrder(sale.Sale{UserID: "staff", PaymentMethod: sale.PaymentCash})
	waitFor(t, &m.admin, 4)
}

func TestSinAdminsNoSeAvisaAlStaff(t *testing.T) {
	m := &fakeMailer{}
	n := NewPurchaseNotifier(fakeUsers{}, m).WithAdmins(fakeAdmins{})
	n.OrderConfirmed(sale.Sale{UserID: "u1", PaymentMethod: sale.PaymentBold, JustCreated: true})
	waitFor(t, &m.confirmed, 1)
	settle(t, &m.admin, 0)
}

func TestCancelacionSolicitadaAvisaAlClienteYALosAdmins(t *testing.T) {
	n, m := newNotifier()
	n.CancelRequested(sale.Sale{UserID: "u1"})
	waitFor(t, &m.cancelReq, 1)
	waitFor(t, &m.alerts, 2)
	if m.lastAlert != out.AlertCancelRequested {
		t.Errorf("alerta = %v, se esperaba AlertCancelRequested", m.lastAlert)
	}
}

// Solo el vencimiento se avisa a los admins: las demás cancelaciones las hace
// el propio staff.
func TestCancelacionPorVencimientoAvisaALosAdmins(t *testing.T) {
	n, m := newNotifier()
	n.PickupCancelled(sale.Sale{UserID: "u1"}, out.CancelByStore)
	waitFor(t, &m.cancelled, 1)
	settle(t, &m.alerts, 0)

	n.PickupCancelled(sale.Sale{UserID: "u1"}, out.CancelExpired)
	waitFor(t, &m.cancelled, 2)
	waitFor(t, &m.alerts, 2)
	if m.lastAlert != out.AlertPickupExpired {
		t.Errorf("alerta = %v, se esperaba AlertPickupExpired", m.lastAlert)
	}
}

func TestPagoAnuladoAvisaALosAdmins(t *testing.T) {
	n, m := newNotifier()
	n.PaymentVoided(sale.Sale{ID: 3, UserID: "u1"})
	waitFor(t, &m.alerts, 2)
	if m.lastAlert != out.AlertPaymentVoided {
		t.Errorf("alerta = %v, se esperaba AlertPaymentVoided", m.lastAlert)
	}
}
