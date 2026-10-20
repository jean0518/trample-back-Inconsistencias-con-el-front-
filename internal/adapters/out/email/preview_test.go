package email

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"trample-back/internal/domain/sale"
	"trample-back/internal/ports/out"
)

// TestWritePreviews escribe un .html por correo para revisarlos en el navegador.
// Solo corre con EMAIL_PREVIEW_DIR=<carpeta>; EMAIL_PREVIEW_LOGO=<url> opcional.
//
//	EMAIL_PREVIEW_DIR=/tmp/correos go test ./internal/adapters/out/email -run Previews
func TestWritePreviews(t *testing.T) {
	dir := os.Getenv("EMAIL_PREVIEW_DIR")
	if dir == "" {
		t.Skip("EMAIL_PREVIEW_DIR sin definir")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var cur string
	m := &Mailer{
		brand: Brand{
			SiteURL: "https://tramplestore.com", LogoURL: os.Getenv("EMAIL_PREVIEW_LOGO"),
			ContactEmail: "hola@trample.co", Location: "Barranquilla, Colombia", Social: "@tramplestore",
			WhatsAppURL: WhatsAppLink("+57 300 123 4567"),
		},
		send: func(_ context.Context, msg message) error {
			return os.WriteFile(filepath.Join(dir, cur+".html"), []byte(msg.HTML), 0o644)
		},
	}
	ctx := context.Background()
	items := []sale.SaleItem{
		{CardName: "Charizard ex", Language: "Español", Quantity: 1, PriceCOP: 185000},
		{CardName: "Pikachu VMAX", Language: "Inglés", Quantity: 2, PriceCOP: 42500},
	}
	paid := sale.Sale{ID: 128, TotalCOP: 290000, ShippingCOP: 20000, Fulfillment: sale.FulfillmentShipping,
		Address: "Calle 84 # 51-20, Apto 302", City: "Barranquilla", Items: items}
	deadline := sale.PickupDeadlineFrom(time.Date(2026, 10, 6, 10, 0, 0, 0, colombia))
	pickup := sale.Sale{ID: 129, TotalCOP: 270000, Fulfillment: sale.FulfillmentPickup, Items: items, PickupDeadline: deadline}
	pendingPickup := pickup
	pendingPickup.Status, pendingPickup.PaymentMethod = sale.StatusPendingPickup, sale.PaymentStore
	paid.PaymentMethod, paid.Status, paid.Phone = sale.PaymentBold, sale.StatusPaid, "300 123 4567"
	buyer := out.OrderBuyer{Name: "Daniel González", Email: "daniel@b.co"}

	steps := []struct {
		name string
		run  func() error
	}{
		{"pago-aprobado", func() error { return m.SendOrderConfirmed(ctx, "a@b.co", "Daniel", paid) }},
		{"pago-rechazado", func() error {
			return m.SendPaymentFailed(ctx, "a@b.co", "Daniel", out.PaymentFailure{Reference: "TRM-1", TotalCOP: 290000})
		}},
		{"pago-anulado", func() error {
			return m.SendPaymentFailed(ctx, "a@b.co", "Daniel", out.PaymentFailure{Reference: "TRM-1", TotalCOP: 290000, Voided: true})
		}},
		{"pago-en-proceso", func() error {
			return m.SendOrderReceived(ctx, "a@b.co", "Daniel", sale.Sale{PaymentReference: "TRM-1", TotalCOP: 290000, ShippingCOP: 20000,
				Fulfillment: sale.FulfillmentShipping, Address: paid.Address, City: paid.City, Items: items}, out.ReceivedPaymentPending)
		}},
		{"pago-en-proceso-vencido", func() error {
			return m.SendPaymentFailed(ctx, "a@b.co", "Daniel", out.PaymentFailure{Reference: "TRM-1", TotalCOP: 290000, Expired: true})
		}},
		{"pago-tardio-recibido", func() error { return m.SendOrderReceived(ctx, "a@b.co", "Daniel", paid, out.ReceivedInReview) }},
		{"pedido-enviado", func() error {
			shipped := paid
			shipped.Carrier, shipped.TrackingNumber = "Servientrega", "2004 5678 9012"
			return m.SendOrderShipped(ctx, "a@b.co", "Daniel", shipped)
		}},
		{"pedido-entregado", func() error { return m.SendOrderDelivered(ctx, "a@b.co", "Daniel", paid) }},
		{"staff-nueva-venta", func() error {
			return m.SendAdminNewOrder(ctx, "a@b.co", buyer, paid)
		}},
		{"staff-revisar", func() error {
			late := paid
			late.RequiresReview, late.PaymentReference = true, "TRM-1"
			return m.SendAdminNewOrder(ctx, "a@b.co", buyer, late)
		}},
		{"staff-pedido-apartado", func() error { return m.SendAdminNewOrder(ctx, "a@b.co", buyer, pendingPickup) }},
		{"staff-cancelacion-solicitada", func() error {
			return m.SendAdminAlert(ctx, "a@b.co", buyer, pendingPickup, out.AlertCancelRequested)
		}},
		{"staff-recogida-vencida", func() error { return m.SendAdminAlert(ctx, "a@b.co", buyer, pickup, out.AlertPickupExpired) }},
		{"staff-pago-anulado", func() error { return m.SendAdminAlert(ctx, "a@b.co", buyer, paid, out.AlertPaymentVoided) }},
		{"recogida-apartada", func() error { return m.SendPickupReserved(ctx, "a@b.co", "Daniel", pickup) }},
		{"recogida-recordatorio", func() error { return m.SendPickupReminder(ctx, "a@b.co", "Daniel", pickup) }},
		{"recogida-pagada", func() error {
			picked := pickup
			picked.PaymentMethod = sale.PaymentStore
			return m.SendOrderConfirmed(ctx, "a@b.co", "Daniel", picked)
		}},
		{"recogida-cancelacion-solicitada", func() error { return m.SendCancelRequested(ctx, "a@b.co", "Daniel", pickup) }},
		{"recogida-cancelacion-rechazada", func() error { return m.SendCancelRejected(ctx, "a@b.co", "Daniel", pickup) }},
		{"recogida-cancelacion-aprobada", func() error {
			return m.SendPickupCancelled(ctx, "a@b.co", "Daniel", pickup, out.CancelRequested)
		}},
		{"recogida-vencida", func() error { return m.SendPickupCancelled(ctx, "a@b.co", "Daniel", pickup, out.CancelExpired) }},
		{"recogida-cancelada", func() error { return m.SendPickupCancelled(ctx, "a@b.co", "Daniel", pickup, out.CancelByStore) }},
		{"verificar-correo", func() error {
			return m.SendEmailVerification(ctx, "a@b.co", "Daniel", "https://tramplestore.com/verificar-email?token=abc123")
		}},
	}
	for _, s := range steps {
		cur = s.name
		if err := s.run(); err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
	}
}
