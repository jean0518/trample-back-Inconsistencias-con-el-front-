package email

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"log/slog"
	"strconv"
	"strings"
	texttemplate "text/template"
	"time"

	"trample-back/internal/domain/sale"
	"trample-back/internal/ports/out"
)

// Mailer arma los correos de compra y los entrega por `send`.
type Mailer struct {
	send  func(ctx context.Context, m message) error
	brand Brand
}

// NewMailer devuelve el mailer de producción respaldado por Resend.
func NewMailer(r *ResendSender, brand Brand) *Mailer { return &Mailer{send: r.send, brand: brand} }

// NewNoopMailer descarta los correos (deja un log): se usa sin RESEND_API_KEY.
func NewNoopMailer() *Mailer {
	return &Mailer{send: func(_ context.Context, m message) error {
		slog.Info("correo no enviado: RESEND_API_KEY sin configurar", slog.String("subject", m.Subject))
		return nil
	}}
}

var _ out.OrderMailer = (*Mailer)(nil)

type orderLine struct {
	Name     string
	Language string
	Quantity int
	Subtotal string
}

// view son los datos que consumen el layout y el contenido de cada correo.
type view struct {
	Brand Brand
	Year  int

	// Cabecera del correo.
	Title     string
	Preheader string // texto que el cliente de correo muestra junto al asunto
	Tag       string // etiqueta de color sobre el título
	Name      string
	Reference string

	// Pedido.
	Lines      []orderLine
	Shipping   string // vacío si es recogida
	Total      string
	TotalLabel string
	Delivery   string

	// Estado.
	Voided   bool
	Expired  bool
	Deadline string // límite para recoger, ya escrito en español

	// Botón de acción (sin URL, no se dibuja).
	ButtonURL   string
	ButtonLabel string
}

func (m *Mailer) base(name, reference string, totalCOP int64) view {
	return view{
		Brand:     m.brand,
		Year:      time.Now().Year(),
		Name:      firstOr(name, "cliente"),
		Reference: reference,
		Total:     formatCOP(totalCOP),
	}
}

func orderLines(items []sale.SaleItem) []orderLine {
	lines := make([]orderLine, 0, len(items))
	for _, it := range items {
		lines = append(lines, orderLine{
			Name: it.CardName, Language: it.Language, Quantity: it.Quantity,
			Subtotal: formatCOP(it.PriceCOP * int64(it.Quantity)),
		})
	}
	return lines
}

func (m *Mailer) SendOrderConfirmed(ctx context.Context, to, name string, s sale.Sale) error {
	v := m.base(name, fmt.Sprintf("#%d", s.ID), s.TotalCOP)
	v.Lines = orderLines(s.Items)
	v.TotalLabel = "Total pagado"
	if s.Fulfillment == sale.FulfillmentShipping {
		v.Shipping = formatCOP(s.ShippingCOP)
		v.Delivery = "Envío a domicilio: " + s.Address + ", " + s.City
	} else {
		v.Delivery = "Recogida en tienda"
	}
	v.Title = "Recibimos tu pago – pedido " + v.Reference
	v.Preheader = "Tu pedido " + v.Reference + " quedó registrado. Total: " + v.Total
	v.Tag = "Pago confirmado"
	v.ButtonURL, v.ButtonLabel = m.brand.siteURL("/cuenta"), "Ver mis pedidos"
	return m.render(ctx, to, v, confirmedHTML, confirmedText)
}

func (m *Mailer) SendPaymentFailed(ctx context.Context, to, name string, f out.PaymentFailure) error {
	v := m.base(name, f.Reference, f.TotalCOP)
	v.Voided = f.Voided
	if f.Voided {
		v.Title = "Tu pago fue anulado – Trample Store"
		v.Tag = "Pago anulado"
		v.Preheader = "Tu pago de " + v.Total + " fue anulado."
		v.ButtonURL, v.ButtonLabel = m.brand.siteURL("/cuenta"), "Ir a mi cuenta"
	} else {
		v.Title = "No pudimos procesar tu pago – Trample Store"
		v.Tag = "Pago no procesado"
		v.Preheader = "No se completó ningún cobro. Puedes intentarlo de nuevo."
		v.ButtonURL, v.ButtonLabel = m.brand.siteURL("/carrito"), "Volver al carrito"
	}
	return m.render(ctx, to, v, failedHTML, failedText)
}

func (m *Mailer) render(ctx context.Context, to string, v view, htmlTpl *template.Template, textTpl *texttemplate.Template) error {
	var h, t bytes.Buffer
	if err := htmlTpl.Execute(&h, v); err != nil {
		return err
	}
	if err := textTpl.Execute(&t, v); err != nil {
		return err
	}
	return m.send(ctx, message{To: to, Subject: v.Title, HTML: h.String(), Text: t.String()})
}

func firstOr(s, def string) string {
	if s = strings.TrimSpace(s); s != "" {
		return s
	}
	return def
}

// formatCOP escribe el valor con punto de miles, como se lee en Colombia.
func formatCOP(v int64) string {
	s := strconv.FormatInt(v, 10)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	return "$" + b.String()
}

var confirmedHTML = newHTML(tag(cPrimary) + `
<h1 style="` + sH1 + `">¡Gracias por tu compra, {{.Name}}!</h1>
<p style="` + sP + `">Recibimos tu pago y tu pedido <strong style="color:` + cText + `">{{.Reference}}</strong> quedó registrado.</p>
` + itemsTable + panel(cPrimary, `{{.Delivery}}`) + buttonHTML)

var confirmedText = newText(`¡Gracias por tu compra, {{.Name}}!

Recibimos tu pago y tu pedido {{.Reference}} quedó registrado.

{{range .Lines}}- {{.Quantity}} x {{.Name}}{{if .Language}} ({{.Language}}){{end}}: {{.Subtotal}}
{{end}}{{if .Shipping}}- Envío: {{.Shipping}}
{{end}}{{.TotalLabel}}: {{.Total}}

{{.Delivery}}{{if .ButtonURL}}

{{.ButtonLabel}}: {{.ButtonURL}}{{end}}`)

var failedHTML = newHTML(tag(cDanger) + `
{{if .Voided}}<h1 style="` + sH1 + `">Tu pago fue anulado</h1>
<p style="` + sP + `">Hola {{.Name}}, tu pago de <strong style="color:` + cText + `">{{.Total}}</strong> (referencia {{.Reference}}) fue anulado. Si ya te lo cobraron, el reembolso lo gestiona tu banco o te contactaremos.</p>
{{else}}<h1 style="` + sH1 + `">No pudimos procesar tu pago</h1>
<p style="` + sP + `">Hola {{.Name}}, tu pago de <strong style="color:` + cText + `">{{.Total}}</strong> (referencia {{.Reference}}) no se completó. <strong style="color:` + cText + `">No se hizo ningún cobro</strong> por este intento.</p>
` + panel(cDanger, `Puedes volver al carrito e intentarlo de nuevo con otro medio de pago. Tus cartas siguen apartadas por un tiempo limitado.`) + `{{end}}` + buttonHTML)

var failedText = newText(`{{if .Voided}}Tu pago fue anulado

Hola {{.Name}}, tu pago de {{.Total}} (referencia {{.Reference}}) fue anulado. Si ya te lo cobraron, el reembolso lo gestiona tu banco o te contactaremos.
{{else}}No pudimos procesar tu pago

Hola {{.Name}}, tu pago de {{.Total}} (referencia {{.Reference}}) no se completó. No se hizo ningún cobro por este intento.

Puedes volver al carrito e intentarlo de nuevo con otro medio de pago. Tus cartas siguen apartadas por un tiempo limitado.
{{end}}{{if .ButtonURL}}
{{.ButtonLabel}}: {{.ButtonURL}}{{end}}`)
