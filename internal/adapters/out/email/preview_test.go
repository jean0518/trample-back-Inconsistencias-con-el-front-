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
		{"recogida-apartada", func() error { return m.SendPickupReserved(ctx, "a@b.co", "Daniel", pickup) }},
		{"recogida-vencida", func() error { return m.SendPickupCancelled(ctx, "a@b.co", "Daniel", pickup, true) }},
		{"recogida-cancelada", func() error { return m.SendPickupCancelled(ctx, "a@b.co", "Daniel", pickup, false) }},
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
