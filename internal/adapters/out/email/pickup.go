package email

import (
	"context"
	"fmt"
	"time"

	"trample-back/internal/domain/sale"
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
	v := m.base(name, fmt.Sprintf("#%d", s.ID), s.TotalCOP)
	v.Lines = orderLines(s.Items, s.ProductItems)
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

func (m *Mailer) SendPickupCancelled(ctx context.Context, to, name string, s sale.Sale, expired bool) error {
	v := m.pickupView(name, s)
	v.Expired = expired
	v.Title = "Tu pedido " + v.Reference + " fue cancelado"
	v.Preheader = "No se hizo ningún cobro. Las cartas volvieron a estar disponibles."
	v.Tag = "Pedido cancelado"
	v.ButtonURL, v.ButtonLabel = m.brand.siteURL("/cartas"), "Volver a la tienda"
	return m.render(ctx, to, v, pickupCancelledHTML, pickupCancelledText)
}

var pickupReservedHTML = newHTML(tag(cPrimary) + `
<h1 style="` + sH1 + `">Tu pedido está apartado, {{.Name}}</h1>
<p style="` + sP + `">Apartamos tu pedido <strong style="color:` + cText + `">{{.Reference}}</strong> para que lo recojas en la tienda. <strong style="color:` + cText + `">El pago se hace allá</strong>, al recogerlo.</p>
` + itemsTable + panel(cPrimary, `<strong style="color:`+cText+`">Recógelo antes del {{.Deadline}}</strong><br>Tienes 2 días hábiles. Si no lo recoges a tiempo, el pedido se cancela y las cartas vuelven a estar disponibles.`) + buttonHTML)

var pickupReservedText = newText(`Tu pedido está apartado, {{.Name}}

Apartamos tu pedido {{.Reference}} para que lo recojas en la tienda. El pago se hace allá, al recogerlo.

{{range .Lines}}- {{.Quantity}} x {{.Name}}{{if .Language}} ({{.Language}}){{end}}: {{.Subtotal}}
{{end}}{{.TotalLabel}}: {{.Total}}

RECÓGELO ANTES DEL {{.Deadline}}
Tienes 2 días hábiles. Si no lo recoges a tiempo, el pedido se cancela y las cartas vuelven a estar disponibles.{{if .ButtonURL}}

{{.ButtonLabel}}: {{.ButtonURL}}{{end}}`)

var pickupCancelledHTML = newHTML(tag(cDanger) + `
<h1 style="` + sH1 + `">Tu pedido fue cancelado</h1>
<p style="` + sP + `">Hola {{.Name}}, tu pedido <strong style="color:` + cText + `">{{.Reference}}</strong> por <strong style="color:` + cText + `">{{.Total}}</strong> {{if .Expired}}se canceló porque no se recogió dentro del plazo de 2 días hábiles{{if .Deadline}} (venció el {{.Deadline}}){{end}}{{else}}fue cancelado por la tienda{{end}}.</p>
` + panel(cDanger, `No se hizo ningún cobro y las cartas volvieron a estar disponibles. Si aún las quieres, puedes volver a hacer el pedido.`) + buttonHTML)

var pickupCancelledText = newText(`Tu pedido fue cancelado

Hola {{.Name}}, tu pedido {{.Reference}} por {{.Total}} {{if .Expired}}se canceló porque no se recogió dentro del plazo de 2 días hábiles{{if .Deadline}} (venció el {{.Deadline}}){{end}}{{else}}fue cancelado por la tienda{{end}}.

No se hizo ningún cobro y las cartas volvieron a estar disponibles. Si aún las quieres, puedes volver a hacer el pedido.{{if .ButtonURL}}

{{.ButtonLabel}}: {{.ButtonURL}}{{end}}`)
