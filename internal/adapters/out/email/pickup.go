package email

import (
	"context"
	"fmt"
	"time"

	"trample-back/internal/domain/sale"
	"trample-back/internal/ports/out"
)

var (
	weekdaysES = [...]string{"domingo", "lunes", "martes", "miércoles", "jueves", "viernes", "sábado"}
	monthsES   = [...]string{"enero", "febrero", "marzo", "abril", "mayo", "junio", "julio", "agosto", "septiembre", "octubre", "noviembre", "diciembre"}
	// Colombia no tiene horario de verano; zona fija para no depender de tzdata.
	colombia = time.FixedZone("COT", -5*60*60)
)

// formatDeadline escribe el límite en hora de Colombia: "martes 13 de octubre de 2026".
// El plazo siempre termina al final del día, así que no se muestra la hora.
func formatDeadline(t time.Time) string {
	t = t.In(colombia)
	return fmt.Sprintf("%s %d de %s de %d", weekdaysES[t.Weekday()], t.Day(), monthsES[t.Month()-1], t.Year())
}

func (m *Mailer) pickupView(name string, s sale.Sale) view {
	v := m.orderView(name, s)
	if !s.PickupDeadline.IsZero() {
		v.Deadline = formatDeadline(s.PickupDeadline)
	}
	return v
}

func (m *Mailer) SendPickupReserved(ctx context.Context, to, name string, s sale.Sale) error {
	v := m.pickupView(name, s)
	v.TotalLabel = "Total a pagar en tienda"
	v.Title = "Tu pedido " + v.Reference + " está apartado – paga en la tienda"
	v.Preheader = "Recógelo y págalo en la tienda antes del " + v.Deadline + "."
	v.Tag = "Pedido apartado"
	v.ButtonURL, v.ButtonLabel = m.brand.siteURL("/cuenta"), "Ver mi pedido"
	return m.render(ctx, to, v, pickupReservedHTML, pickupReservedText)
}

func (m *Mailer) SendPickupReminder(ctx context.Context, to, name string, s sale.Sale) error {
	v := m.pickupView(name, s)
	v.TotalLabel = "Total a pagar en tienda"
	v.Title = "Recuerda recoger tu pedido " + v.Reference + " – vence el " + v.Deadline
	v.Preheader = "Tu pedido sigue apartado hasta el " + v.Deadline + "."
	v.Tag = "Recordatorio"
	v.ButtonURL, v.ButtonLabel = m.brand.siteURL("/cuenta"), "Ver mi pedido"
	return m.render(ctx, to, v, pickupReminderHTML, pickupReminderText)
}

func (m *Mailer) SendPickupCancelled(ctx context.Context, to, name string, s sale.Sale, reason out.CancelReason) error {
	v := m.pickupView(name, s)
	v.Expired = reason == out.CancelExpired
	v.ByStore = reason == out.CancelByStore
	v.Approved = reason == out.CancelRequested
	v.Title = "Tu pedido " + v.Reference + " fue cancelado"
	v.Preheader = "No se hizo ningún cobro. Las cartas volvieron a estar disponibles."
	v.Tag = "Pedido cancelado"
	v.ButtonURL, v.ButtonLabel = m.brand.siteURL("/cartas"), "Volver a la tienda"
	return m.render(ctx, to, v, pickupCancelledHTML, pickupCancelledText)
}

func (m *Mailer) SendCancelRequested(ctx context.Context, to, name string, s sale.Sale) error {
	v := m.pickupView(name, s)
	v.Title = "Recibimos tu solicitud para cancelar el pedido " + v.Reference
	v.Preheader = "La revisamos y te avisamos por correo."
	v.Tag = "Cancelación solicitada"
	v.ButtonURL, v.ButtonLabel = m.brand.siteURL("/cuenta"), "Ver mi pedido"
	return m.render(ctx, to, v, cancelRequestedHTML, cancelRequestedText)
}

func (m *Mailer) SendCancelRejected(ctx context.Context, to, name string, s sale.Sale) error {
	v := m.pickupView(name, s)
	v.TotalLabel = "Total a pagar en tienda"
	v.Title = "No pudimos cancelar tu pedido " + v.Reference
	v.Preheader = "Tu pedido sigue apartado. Escríbenos si necesitas ayuda."
	v.Tag = "Cancelación no aprobada"
	v.ButtonURL, v.ButtonLabel = m.brand.siteURL("/cuenta"), "Ver mi pedido"
	return m.render(ctx, to, v, cancelRejectedHTML, cancelRejectedText)
}

var pickupReservedHTML = newHTML(tag(cPrimary) + `
<h1 style="` + sH1 + `">Tu pedido está apartado, {{.Name}}</h1>
<p style="` + sP + `">Apartamos tu pedido <strong style="color:` + cText + `">{{.Reference}}</strong> para que lo recojas en la tienda. <strong style="color:` + cText + `">El pago se hace allá</strong>, al recogerlo.</p>
` + itemsTable + panel(cPrimary, `<strong style="color:`+cText+`">Recógelo antes del {{.Deadline}}</strong><br>Tienes 2 días hábiles. Si no lo recoges a tiempo, el pedido se cancela y las cartas vuelven a estar disponibles.`) + `
<p style="` + sSmall + `;margin:0 0 16px">¿Cambiaste de idea? Puedes solicitar la cancelación desde tu cuenta.</p>` + buttonHTML)

var pickupReservedText = newText(`Tu pedido está apartado, {{.Name}}

Apartamos tu pedido {{.Reference}} para que lo recojas en la tienda. El pago se hace allá, al recogerlo.

` + linesText + `

RECÓGELO ANTES DEL {{.Deadline}}
Tienes 2 días hábiles. Si no lo recoges a tiempo, el pedido se cancela y las cartas vuelven a estar disponibles.

¿Cambiaste de idea? Puedes solicitar la cancelación desde tu cuenta.` + buttonText)

var pickupReminderHTML = newHTML(tag(cPrimary) + `
<h1 style="` + sH1 + `">Tu pedido te espera, {{.Name}}</h1>
<p style="` + sP + `">Te recordamos que tu pedido <strong style="color:` + cText + `">{{.Reference}}</strong> sigue apartado en la tienda.</p>
` + itemsTable + panel(cPrimary, `<strong style="color:`+cText+`">Recógelo antes del {{.Deadline}}</strong><br>Si no lo recoges a tiempo, el pedido se cancela y las cartas vuelven a estar disponibles.`) + helpHTML("¿No alcanzas a pasar?") + buttonHTML)

var pickupReminderText = newText(`Tu pedido te espera, {{.Name}}

Te recordamos que tu pedido {{.Reference}} sigue apartado en la tienda.

` + linesText + `

RECÓGELO ANTES DEL {{.Deadline}}
Si no lo recoges a tiempo, el pedido se cancela y las cartas vuelven a estar disponibles.

` + helpText("¿No alcanzas a pasar?") + buttonText)

// cancelledReason explica el motivo de la cancelación (mismo texto en HTML y
// texto plano).
const cancelledReason = `{{if .Expired}}se canceló porque no se recogió dentro del plazo de 2 días hábiles{{if .Deadline}} (venció el {{.Deadline}}){{end}}{{else if .Approved}}se canceló, tal como lo solicitaste{{else}}fue cancelado por la tienda{{end}}`

var pickupCancelledHTML = newHTML(tag(cDanger) + `
<h1 style="` + sH1 + `">Tu pedido fue cancelado</h1>
<p style="` + sP + `">Hola {{.Name}}, tu pedido <strong style="color:` + cText + `">{{.Reference}}</strong> por <strong style="color:` + cText + `">{{.Total}}</strong> ` + cancelledReason + `.</p>
` + panel(cDanger, `No se hizo ningún cobro y las cartas volvieron a estar disponibles. Si aún las quieres, puedes volver a hacer el pedido.`) + `{{if .ByStore}}` + helpHTML("¿Tienes dudas?") + `{{end}}` + buttonHTML)

var pickupCancelledText = newText(`Tu pedido fue cancelado

Hola {{.Name}}, tu pedido {{.Reference}} por {{.Total}} ` + cancelledReason + `.

No se hizo ningún cobro y las cartas volvieron a estar disponibles. Si aún las quieres, puedes volver a hacer el pedido.{{if .ByStore}}

` + helpText("¿Tienes dudas?") + `{{end}}` + buttonText)

var cancelRequestedHTML = newHTML(tag(cPrimary) + `
<h1 style="` + sH1 + `">Recibimos tu solicitud, {{.Name}}</h1>
<p style="` + sP + `">Pediste cancelar tu pedido <strong style="color:` + cText + `">{{.Reference}}</strong> por <strong style="color:` + cText + `">{{.Total}}</strong>. La vamos a revisar y te avisaremos por correo cuando quede cancelado.</p>
` + panel(cPrimary, `Mientras tanto el pedido sigue apartado. No tienes que pagar nada.`) + helpHTML("¿Fue un error o tienes dudas?") + buttonHTML)

var cancelRequestedText = newText(`Recibimos tu solicitud, {{.Name}}

Pediste cancelar tu pedido {{.Reference}} por {{.Total}}. La vamos a revisar y te avisaremos por correo cuando quede cancelado.

Mientras tanto el pedido sigue apartado. No tienes que pagar nada.

` + helpText("¿Fue un error o tienes dudas?") + buttonText)

var cancelRejectedHTML = newHTML(tag(cDanger) + `
<h1 style="` + sH1 + `">No pudimos cancelar tu pedido</h1>
<p style="` + sP + `">Hola {{.Name}}, revisamos tu solicitud y no pudimos cancelar el pedido <strong style="color:` + cText + `">{{.Reference}}</strong>. Sigue apartado para que lo recojas en la tienda.</p>
` + itemsTable + `{{if .Deadline}}` + panel(cPrimary, `<strong style="color:`+cText+`">Recógelo antes del {{.Deadline}}</strong>`) + `{{end}}` + helpHTML("¿Quieres saber por qué o necesitas ayuda?") + buttonHTML)

var cancelRejectedText = newText(`No pudimos cancelar tu pedido

Hola {{.Name}}, revisamos tu solicitud y no pudimos cancelar el pedido {{.Reference}}. Sigue apartado para que lo recojas en la tienda.

` + linesText + `{{if .Deadline}}

RECÓGELO ANTES DEL {{.Deadline}}{{end}}

` + helpText("¿Quieres saber por qué o necesitas ayuda?") + buttonText)
