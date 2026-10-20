package email

import (
	"bytes"
	"context"
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
	ByStore  bool   // cancelado por decisión de la tienda
	Approved bool   // cancelación aprobada a pedido del cliente
	PickedUp bool   // confirmado al pagarse y recogerse en la tienda
	Review   bool   // recibido, pero hay que confirmar la disponibilidad
	Deadline string // límite para recoger, ya escrito en español

	// Despacho.
	Carrier  string
	Tracking string

	// Botón de acción (sin URL, no se dibuja).
	ButtonURL   string
	ButtonLabel string
}

// orderView es la base de los correos de un pedido: referencia, líneas, total y
// entrega. Un checkout que todavía no es pedido se identifica por la referencia
// de pago.
func (m *Mailer) orderView(name string, s sale.Sale) view {
	ref := s.PaymentReference
	if s.ID != 0 {
		ref = s.Reference()
	}
	v := m.base(name, ref, s.TotalCOP)
	v.Lines = orderLines(s.Items, s.ProductItems)
	if s.Fulfillment == sale.FulfillmentShipping {
		v.Shipping = formatCOP(s.ShippingCOP)
		v.Delivery = "Envío a domicilio: " + s.Address + ", " + s.City
	} else {
		v.Delivery = "Recogida en tienda"
	}
	return v
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

// orderLines arma las líneas del correo: primero las cartas y después los
// productos propios de la tienda (que no llevan idioma).
func orderLines(items []sale.SaleItem, productItems []sale.ProductItem) []orderLine {
	lines := make([]orderLine, 0, len(items)+len(productItems))
	for _, it := range items {
		lines = append(lines, orderLine{
			Name: it.CardName, Language: it.Language, Quantity: it.Quantity,
			Subtotal: formatCOP(it.PriceCOP * int64(it.Quantity)),
		})
	}
	for _, it := range productItems {
		lines = append(lines, orderLine{
			Name: it.Title, Quantity: it.Quantity,
			Subtotal: formatCOP(it.PriceCOP * int64(it.Quantity)),
		})
	}
	return lines
}

func (m *Mailer) SendOrderConfirmed(ctx context.Context, to, name string, s sale.Sale) error {
	v := m.orderView(name, s)
	v.TotalLabel = "Total pagado"
	v.ButtonURL, v.ButtonLabel = m.brand.siteURL("/cuenta"), "Ver mis pedidos"
	if s.PaymentMethod == sale.PaymentStore {
		v.PickedUp = true
		v.Title = "Gracias por recoger tu pedido " + v.Reference
		v.Preheader = "Pagaste " + v.Total + " en la tienda. ¡Disfruta tus cartas!"
		v.Tag = "Pedido entregado"
		return m.render(ctx, to, v, confirmedHTML, confirmedText)
	}
	v.Title = "Pedido " + v.Reference + " confirmado – recibimos tu pago"
	v.Preheader = "Tu pedido " + v.Reference + " está confirmado. Total: " + v.Total
	v.Tag = "Pedido confirmado"
	return m.render(ctx, to, v, confirmedHTML, confirmedText)
}

func (m *Mailer) SendOrderReceived(ctx context.Context, to, name string, s sale.Sale, reason out.ReceivedReason) error {
	v := m.orderView(name, s)
	v.Review = reason == out.ReceivedInReview
	v.Tag = "Pedido recibido"
	v.Title = "Recibimos tu pedido " + v.Reference
	v.ButtonURL, v.ButtonLabel = m.brand.siteURL("/cuenta"), "Ver mis pedidos"
	if v.Review {
		v.TotalLabel = "Total pagado"
		v.Preheader = "Recibimos tu pago. Estamos confirmando la disponibilidad de tu pedido."
	} else {
		v.TotalLabel = "Total a pagar"
		v.Preheader = "Estamos esperando la confirmación de tu pago."
	}
	return m.render(ctx, to, v, receivedHTML, receivedText)
}

func (m *Mailer) SendPaymentFailed(ctx context.Context, to, name string, f out.PaymentFailure) error {
	v := m.base(name, f.Reference, f.TotalCOP)
	v.Voided = f.Voided
	v.Expired = f.Expired
	if f.Expired {
		v.Title = "Tu pedido fue cancelado: no recibimos el pago – Trample Store"
		v.Tag = "Pedido cancelado"
		v.Preheader = "El pago no se confirmó a tiempo. No se hizo ningún cobro."
		v.ButtonURL, v.ButtonLabel = m.brand.siteURL("/cartas"), "Volver a la tienda"
	} else if f.Voided {
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
	return m.renderData(ctx, to, v.Title, v, htmlTpl, textTpl)
}

// renderData es render para plantillas que reciben más datos que el view común
// (p. ej. el correo del staff); `subject` sale de su Title.
func (m *Mailer) renderData(ctx context.Context, to, subject string, data any, htmlTpl *template.Template, textTpl *texttemplate.Template) error {
	var h, t bytes.Buffer
	if err := htmlTpl.Execute(&h, data); err != nil {
		return err
	}
	if err := textTpl.Execute(&t, data); err != nil {
		return err
	}
	return m.send(ctx, message{To: to, Subject: subject, HTML: h.String(), Text: t.String()})
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

// linesText es el detalle del pedido en texto plano.
const linesText = `{{range .Lines}}- {{.Quantity}} x {{.Name}}{{if .Language}} ({{.Language}}){{end}}: {{.Subtotal}}
{{end}}{{if .Shipping}}- Envío: {{.Shipping}}
{{end}}{{.TotalLabel}}: {{.Total}}`

const buttonText = `{{if .ButtonURL}}

{{.ButtonLabel}}: {{.ButtonURL}}{{end}}`

var confirmedHTML = newHTML(tag(cPrimary) + `
{{if .PickedUp}}<h1 style="` + sH1 + `">¡Gracias por tu compra, {{.Name}}!</h1>
<p style="` + sP + `">Recibimos tu pago en la tienda y entregamos tu pedido <strong style="color:` + cText + `">{{.Reference}}</strong>. ¡Disfruta tus cartas!</p>
` + itemsTable + `{{else}}<h1 style="` + sH1 + `">¡Tu pedido está confirmado, {{.Name}}!</h1>
<p style="` + sP + `">Recibimos tu pago y tu pedido <strong style="color:` + cText + `">{{.Reference}}</strong> quedó confirmado.</p>
` + itemsTable + panel(cPrimary, `{{.Delivery}}`) + helpHTML("¿Necesitas cambiar o cancelar tu pedido?") + `{{end}}` + buttonHTML)

var confirmedText = newText(`{{if .PickedUp}}¡Gracias por tu compra, {{.Name}}!

Recibimos tu pago en la tienda y entregamos tu pedido {{.Reference}}. ¡Disfruta tus cartas!

` + linesText + `{{else}}¡Tu pedido está confirmado, {{.Name}}!

Recibimos tu pago y tu pedido {{.Reference}} quedó confirmado.

` + linesText + `

{{.Delivery}}

` + helpText("¿Necesitas cambiar o cancelar tu pedido?") + `{{end}}` + buttonText)

var receivedHTML = newHTML(tag(cPrimary) + `
<h1 style="` + sH1 + `">Recibimos tu pedido, {{.Name}}</h1>
{{if .Review}}<p style="` + sP + `">Recibimos tu pago del pedido <strong style="color:` + cText + `">{{.Reference}}</strong>. Antes de confirmarlo estamos revisando que todas las cartas sigan disponibles.</p>
` + itemsTable + panel(cPrimary, `Te enviaremos la confirmación del pedido en cuanto terminemos la revisión. Si algo no está disponible, te contactamos para resolverlo.`) + `{{else}}<p style="` + sP + `">Recibimos tu pedido <strong style="color:` + cText + `">{{.Reference}}</strong> y <strong style="color:` + cText + `">estamos esperando la confirmación de tu pago</strong>. Algunos medios, como PSE, tardan un poco en confirmarse.</p>
` + itemsTable + panel(cPrimary, `Tus cartas quedan apartadas hasta 2 horas. Cuando el pago se confirme te enviaremos la confirmación del pedido; si no se confirma en ese tiempo, el pedido se cancela y no se cobra nada.`) + `{{end}}` + helpHTML("¿Tienes dudas?") + buttonHTML)

var receivedText = newText(`Recibimos tu pedido, {{.Name}}

{{if .Review}}Recibimos tu pago del pedido {{.Reference}}. Antes de confirmarlo estamos revisando que todas las cartas sigan disponibles.

` + linesText + `

Te enviaremos la confirmación del pedido en cuanto terminemos la revisión. Si algo no está disponible, te contactamos para resolverlo.{{else}}Recibimos tu pedido {{.Reference}} y estamos esperando la confirmación de tu pago. Algunos medios, como PSE, tardan un poco en confirmarse.

` + linesText + `

Tus cartas quedan apartadas hasta 2 horas. Cuando el pago se confirme te enviaremos la confirmación del pedido; si no se confirma en ese tiempo, el pedido se cancela y no se cobra nada.{{end}}

` + helpText("¿Tienes dudas?") + buttonText)

var failedHTML = newHTML(tag(cDanger) + `
{{if .Expired}}<h1 style="` + sH1 + `">Tu pedido fue cancelado</h1>
<p style="` + sP + `">Hola {{.Name}}, no recibimos la confirmación de tu pago de <strong style="color:` + cText + `">{{.Total}}</strong> (referencia {{.Reference}}) dentro de las 2 horas, así que cancelamos el pedido y las cartas volvieron a estar disponibles.</p>
` + panel(cDanger, `No se hizo ningún cobro. Si tu banco sí te descontó el dinero, escríbenos con la referencia y lo resolvemos.`) + helpHTML("¿Necesitas ayuda?") + `
{{else if .Voided}}<h1 style="` + sH1 + `">Tu pago fue anulado</h1>
<p style="` + sP + `">Hola {{.Name}}, tu pago de <strong style="color:` + cText + `">{{.Total}}</strong> (referencia {{.Reference}}) fue anulado. Si ya te lo cobraron, el reembolso lo gestiona tu banco o te contactaremos.</p>
{{else}}<h1 style="` + sH1 + `">No pudimos procesar tu pago</h1>
<p style="` + sP + `">Hola {{.Name}}, tu pago de <strong style="color:` + cText + `">{{.Total}}</strong> (referencia {{.Reference}}) no se completó. <strong style="color:` + cText + `">No se hizo ningún cobro</strong> por este intento.</p>
` + panel(cDanger, `Puedes volver al carrito e intentarlo de nuevo con otro medio de pago. Tus cartas siguen apartadas por un tiempo limitado.`) + `{{end}}` + buttonHTML)

var failedText = newText(`{{if .Expired}}Tu pedido fue cancelado

Hola {{.Name}}, no recibimos la confirmación de tu pago de {{.Total}} (referencia {{.Reference}}) dentro de las 2 horas, así que cancelamos el pedido y las cartas volvieron a estar disponibles.

No se hizo ningún cobro. Si tu banco sí te descontó el dinero, escríbenos con la referencia y lo resolvemos.

` + helpText("¿Necesitas ayuda?") + `
{{else if .Voided}}Tu pago fue anulado

Hola {{.Name}}, tu pago de {{.Total}} (referencia {{.Reference}}) fue anulado. Si ya te lo cobraron, el reembolso lo gestiona tu banco o te contactaremos.
{{else}}No pudimos procesar tu pago

Hola {{.Name}}, tu pago de {{.Total}} (referencia {{.Reference}}) no se completó. No se hizo ningún cobro por este intento.

Puedes volver al carrito e intentarlo de nuevo con otro medio de pago. Tus cartas siguen apartadas por un tiempo limitado.
{{end}}{{if .ButtonURL}}
{{.ButtonLabel}}: {{.ButtonURL}}{{end}}`)
