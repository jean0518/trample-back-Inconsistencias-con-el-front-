package sale

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"trample-back/internal/domain/auth"
	"trample-back/internal/domain/sale"
	"trample-back/internal/ports/out"
)

type fakeUsers struct{}

func (fakeUsers) FindByID(_ context.Context, id string) (auth.User, error) {
	return auth.User{ID: id, Email: "ana@example.com", FirstName: "Ana"}, nil
}

type fakeMailer struct{ confirmed, failed, reserved, cancelled atomic.Int32 }

func (m *fakeMailer) SendPickupReserved(context.Context, string, string, sale.Sale) error {
	m.reserved.Add(1)
	return nil
}
func (m *fakeMailer) SendPickupCancelled(context.Context, string, string, sale.Sale, bool) error {
	m.cancelled.Add(1)
	return nil
}

func (m *fakeMailer) SendOrderConfirmed(context.Context, string, string, sale.Sale) error {
	m.confirmed.Add(1)
	return nil
}
func (m *fakeMailer) SendPaymentFailed(context.Context, string, string, out.PaymentFailure) error {
	m.failed.Add(1)
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

func TestOrderConfirmedSoloAvisaCuandoElPedidoSeAcabaDeCrear(t *testing.T) {
	m := &fakeMailer{}
	n := NewPurchaseNotifier(fakeUsers{}, m)

	n.OrderConfirmed(sale.Sale{UserID: "u1", JustCreated: false}) // webhook repetido
	n.OrderConfirmed(sale.Sale{UserID: "u1", JustCreated: true})
	waitFor(t, &m.confirmed, 1)
	time.Sleep(50 * time.Millisecond)
	if got := m.confirmed.Load(); got != 1 {
		t.Fatalf("correos enviados = %d, se esperaba 1", got)
	}
}

func TestNotificadorNilNoHaceNada(t *testing.T) {
	var n *PurchaseNotifier
	n.OrderConfirmed(sale.Sale{JustCreated: true})
	n.PaymentFailed("u1", out.PaymentFailure{})
}

func TestPaymentFailedEnviaCorreo(t *testing.T) {
	m := &fakeMailer{}
	NewPurchaseNotifier(fakeUsers{}, m).PaymentFailed("u1", out.PaymentFailure{Reference: "R1", TotalCOP: 1000})
	waitFor(t, &m.failed, 1)
}
