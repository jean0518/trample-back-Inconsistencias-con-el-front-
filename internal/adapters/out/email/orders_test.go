package email

import (
	"context"
	"strings"
	"testing"

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
		ID: 7, TotalCOP: 1234567, ShippingCOP: 20000, Fulfillment: sale.FulfillmentShipping,
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
