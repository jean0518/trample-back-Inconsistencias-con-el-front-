package email

import (
	"context"
	"html/template"
	"strings"
	"testing"
	"time"

	"trample-back/internal/domain/sale"
	"trample-back/internal/ports/out"
)

func TestRenderCorreos(t *testing.T) {
	var got message
	m := &Mailer{
		send: func(_ context.Context, msg message) error { got = msg; return nil },
		brand: Brand{
			SiteURL: "https://tramplestore.com", LogoURL: "https://tramplestore.com/logos/email-logo.png",
			ContactEmail: "hola@trample.co", Location: "Barranquilla, Colombia",
		},
	}

	err := m.SendOrderConfirmed(context.Background(), "a@b.co", "Ana <b>", sale.Sale{
		PaymentMethod: sale.PaymentBold,
		ID:            7, TotalCOP: 1234567, ShippingCOP: 20000, Fulfillment: sale.FulfillmentShipping,
		Address: "Calle 1", City: "Cali",
		Items: []sale.SaleItem{{CardName: "Pikachu", Language: "es", Quantity: 2, PriceCOP: 10000}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"#7", "$1.234.567", "$20.000", "Pikachu"} {
		if !strings.Contains(got.HTML, want) || !strings.Contains(got.Text, want) {
			t.Errorf("falta %q en el correo", want)
		}
	}

	// Todos los correos deben salir envueltos en el layout (logo, pie y botón):
	// una plantilla que devuelve solo el contenido se ve rota en el cliente.
	for _, want := range []string{"<!doctype html>", "email-logo.png", "hola@trample.co", "https://tramplestore.com/cuenta"} {
		if !strings.Contains(got.HTML, want) {
			t.Errorf("el HTML no incluye %q: falta el layout", want)
		}
	}
	for _, want := range []string{"hola@trample.co", "https://tramplestore.com/cuenta"} {
		if !strings.Contains(got.Text, want) {
			t.Errorf("el texto plano no incluye %q", want)
		}
	}

	if err := m.SendPaymentFailed(context.Background(), "a@b.co", "<b>x</b>", out.PaymentFailure{Reference: "R1", TotalCOP: 5000}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.HTML, "<b>x</b>") {
		t.Error("el nombre del cliente no se escapó en el HTML")
	}
	if !strings.Contains(got.Text, "$5.000") {
		t.Errorf("correo de pago fallido inesperado: %s", got.Text)
	}
}

func TestRenderVerificacion(t *testing.T) {
	var got message
	m := &Mailer{
		send:  func(_ context.Context, msg message) error { got = msg; return nil },
		brand: Brand{SiteURL: "https://tramplestore.com", ContactEmail: "hola@trample.co"},
	}
	link := "https://tramplestore.com/verificar-email?token=abc123"
	if err := m.SendEmailVerification(context.Background(), "a@b.co", "<i>Ana</i>", link); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.HTML, "<!doctype html>") || !strings.Contains(got.HTML, `href="`+link+`"`) {
		t.Errorf("el HTML debe llevar el layout y el botón con el enlace: %s", got.HTML)
	}
	if !strings.Contains(got.Text, link) {
		t.Errorf("el texto plano debe incluir el enlace: %s", got.Text)
	}
	if strings.Contains(got.HTML, "<i>Ana</i>") {
		t.Error("el nombre no se escapó en el HTML")
	}
}

func TestOrderLinesIncluyeProductosSinIdioma(t *testing.T) {
	lines := orderLines(
		[]sale.SaleItem{{CardName: "Pikachu", Language: "Inglés", Quantity: 2, PriceCOP: 1000}},
		[]sale.ProductItem{{Title: "Caja de sobres", Quantity: 1, PriceCOP: 450000}},
	)
	if len(lines) != 2 {
		t.Fatalf("líneas = %d, se esperaban 2", len(lines))
	}
	if lines[0].Name != "Pikachu" || lines[0].Language != "Inglés" {
		t.Errorf("carta: %+v", lines[0])
	}
	if lines[1].Name != "Caja de sobres" || lines[1].Language != "" || lines[1].Quantity != 1 {
		t.Errorf("producto: %+v", lines[1])
	}
	if lines[1].Subtotal == "" {
		t.Error("el producto debe llevar subtotal")
	}
}

func TestRenderAvisoStaff(t *testing.T) {
	var got message
	m := &Mailer{
		send:  func(_ context.Context, msg message) error { got = msg; return nil },
		brand: Brand{SiteURL: "https://tramplestore.com", ContactEmail: "hola@trample.co"},
	}
	err := m.SendAdminNewOrder(context.Background(), "admin@trample.co", out.OrderBuyer{Name: "Ana <b>", Email: "ana@b.co"}, sale.Sale{
		ID: 9, TotalCOP: 45000, Fulfillment: sale.FulfillmentShipping, Address: "Calle 1", City: "Cali", Phone: "3001234567",
		PaymentMethod: sale.PaymentBold, PaymentReference: "TRM-1", Status: sale.StatusPaid, RequiresReview: true,
		Items: []sale.SaleItem{{CardName: "Pikachu", Language: "es", Quantity: 1, PriceCOP: 20000}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.To != "admin@trample.co" || !strings.HasPrefix(got.Subject, "[REVISAR] Nueva venta #9") {
		t.Errorf("destinatario o asunto inesperado: %q / %q", got.To, got.Subject)
	}
	for _, want := range []string{"#9", "$45.000", "Pikachu", "ana@b.co", "3001234567", "TRM-1", "En línea (Bold)", "Calle 1", "https://tramplestore.com/admin/pedidos"} {
		if !strings.Contains(got.HTML, want) || !strings.Contains(got.Text, want) {
			t.Errorf("falta %q en el aviso al staff", want)
		}
	}
	if strings.Contains(got.HTML, "Ana <b>") {
		t.Error("el nombre del comprador no se escapó en el HTML")
	}
}

// testMailer devuelve un mailer que guarda el último correo enviado.
func testMailer(brand Brand) (*Mailer, *message) {
	got := &message{}
	return &Mailer{send: func(_ context.Context, msg message) error { *got = msg; return nil }, brand: brand}, got
}

var testBrand = Brand{SiteURL: "https://tramplestore.com", ContactEmail: "hola@trample.co", WhatsAppURL: "https://wa.me/573001234567"}

func TestWhatsAppLink(t *testing.T) {
	if got := WhatsAppLink("+57 300 123-4567"); got != "https://wa.me/573001234567" {
		t.Errorf("WhatsAppLink = %q", got)
	}
	if got := WhatsAppLink(""); got != "" {
		t.Errorf("sin número debe quedar vacío, quedó %q", got)
	}
}

// Cada correo lleva su asunto y sus datos clave, en HTML y en texto plano.
func TestRenderCorreosDelCicloDeCompra(t *testing.T) {
	m, got := testMailer(testBrand)
	ctx := context.Background()
	items := []sale.SaleItem{{CardName: "Pikachu", Language: "es", Quantity: 1, PriceCOP: 20000}}
	paid := sale.Sale{ID: 7, TotalCOP: 45000, ShippingCOP: 25000, Fulfillment: sale.FulfillmentShipping, Address: "Calle 1", City: "Cali",
		PaymentMethod: sale.PaymentBold, Status: sale.StatusPaid, Items: items}
	pickup := sale.Sale{ID: 8, TotalCOP: 20000, Fulfillment: sale.FulfillmentPickup, PaymentMethod: sale.PaymentStore,
		Status: sale.StatusPendingPickup, Items: items, PickupDeadline: time.Date(2026, 10, 13, 23, 59, 59, 0, colombia)}
	shipped := paid
	shipped.Carrier, shipped.TrackingNumber = "Servientrega", "ABC123"
	picked := pickup
	picked.Status = sale.StatusPaid

	cases := []struct {
		name    string
		send    func() error
		subject string
		want    []string
	}{
		{"confirmado en línea", func() error { return m.SendOrderConfirmed(ctx, "a@b.co", "Ana", paid) },
			"Pedido #7 confirmado", []string{"#7", "$45.000", "wa.me/573001234567", "cancelar"}},
		{"recogida pagada", func() error { return m.SendOrderConfirmed(ctx, "a@b.co", "Ana", picked) },
			"Gracias por recoger tu pedido #8", []string{"#8", "entregamos tu pedido"}},
		{"pago en proceso", func() error {
			return m.SendOrderReceived(ctx, "a@b.co", "Ana", sale.Sale{PaymentReference: "TRM-1", TotalCOP: 45000, Items: items}, out.ReceivedPaymentPending)
		}, "Recibimos tu pedido TRM-1", []string{"TRM-1", "2 horas", "esperando la confirmación"}},
		{"pago tardío", func() error { return m.SendOrderReceived(ctx, "a@b.co", "Ana", paid, out.ReceivedInReview) },
			"Recibimos tu pedido #7", []string{"#7", "disponibles", "confirmación del pedido"}},
		{"pago vencido", func() error {
			return m.SendPaymentFailed(ctx, "a@b.co", "Ana", out.PaymentFailure{Reference: "TRM-1", TotalCOP: 45000, Expired: true})
		}, "Tu pedido fue cancelado: no recibimos el pago", []string{"TRM-1", "2 horas", "No se hizo ningún cobro"}},
		{"recordatorio", func() error { return m.SendPickupReminder(ctx, "a@b.co", "Ana", pickup) },
			"Recuerda recoger tu pedido #8", []string{"martes 13 de octubre de 2026"}},
		{"cancelación solicitada", func() error { return m.SendCancelRequested(ctx, "a@b.co", "Ana", pickup) },
			"Recibimos tu solicitud para cancelar el pedido #8", []string{"#8", "sigue apartado"}},
		{"cancelación rechazada", func() error { return m.SendCancelRejected(ctx, "a@b.co", "Ana", pickup) },
			"No pudimos cancelar tu pedido #8", []string{"#8", "wa.me/573001234567"}},
		{"cancelación aprobada", func() error { return m.SendPickupCancelled(ctx, "a@b.co", "Ana", pickup, out.CancelRequested) },
			"Tu pedido #8 fue cancelado", []string{"tal como lo solicitaste"}},
		{"recogida vencida", func() error { return m.SendPickupCancelled(ctx, "a@b.co", "Ana", pickup, out.CancelExpired) },
			"Tu pedido #8 fue cancelado", []string{"plazo de 2 días hábiles"}},
		{"enviado", func() error { return m.SendOrderShipped(ctx, "a@b.co", "Ana", shipped) },
			"Tu pedido #7 va en camino", []string{"Servientrega", "ABC123"}},
		{"entregado", func() error { return m.SendOrderDelivered(ctx, "a@b.co", "Ana", paid) },
			"Tu pedido #7 fue entregado", []string{"#7", "fue entregado"}},
		{"admin: cancelación solicitada", func() error {
			return m.SendAdminAlert(ctx, "admin@trample.co", out.OrderBuyer{Name: "Ana", Email: "ana@b.co"}, pickup, out.AlertCancelRequested)
		}, "El cliente pidió cancelar el pedido #8", []string{"ana@b.co", "Apruébalo o recházalo"}},
		{"admin: recogida vencida", func() error {
			return m.SendAdminAlert(ctx, "admin@trample.co", out.OrderBuyer{Name: "Ana"}, pickup, out.AlertPickupExpired)
		}, "Pedido #8 vencido sin recoger", []string{"volvió al inventario"}},
		{"admin: pago anulado", func() error {
			return m.SendAdminAlert(ctx, "admin@trample.co", out.OrderBuyer{Name: "Ana"}, paid, out.AlertPaymentVoided)
		}, "⚠️ NO DESPACHAR – pago anulado del pedido #7", []string{"No despaches", "#7"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			*got = message{}
			if err := c.send(); err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(got.Subject, c.subject) {
				t.Errorf("asunto = %q, se esperaba que empezara por %q", got.Subject, c.subject)
			}
			if !strings.Contains(got.HTML, "<!doctype html>") {
				t.Error("el HTML no lleva el layout")
			}
			for _, w := range c.want {
				// El HTML escapa algunos caracteres; se compara contra el texto
				// plano y, en el HTML, contra la versión escapada.
				if !strings.Contains(got.Text, w) {
					t.Errorf("falta %q en el texto plano", w)
				}
				if !strings.Contains(got.HTML, w) && !strings.Contains(got.HTML, template.HTMLEscapeString(w)) {
					t.Errorf("falta %q en el HTML", w)
				}
			}
		})
	}
}

// Sin WhatsApp configurado, los correos remiten al correo de contacto.
func TestSinWhatsAppRemiteAlCorreo(t *testing.T) {
	m, got := testMailer(Brand{SiteURL: "https://tramplestore.com", ContactEmail: "hola@trample.co"})
	if err := m.SendCancelRejected(context.Background(), "a@b.co", "Ana", sale.Sale{ID: 1}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.HTML, "wa.me") || !strings.Contains(got.HTML, "mailto:hola@trample.co") {
		t.Error("sin WhatsApp el correo debe invitar a escribir al correo de contacto")
	}
}
