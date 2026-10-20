package email

import (
	"context"

	"trample-back/internal/domain/sale"
)

func (m *Mailer) SendOrderShipped(ctx context.Context, to, name string, s sale.Sale) error {
	v := m.orderView(name, s)
	v.TotalLabel = "Total pagado"
	v.Carrier, v.Tracking = s.Carrier, s.TrackingNumber
	v.Title = "Tu pedido " + v.Reference + " va en camino"
	v.Preheader = "Lo enviamos"
	if v.Carrier != "" {
		v.Preheader += " con " + v.Carrier
	}
	if v.Tracking != "" {
		v.Preheader += ". Guía: " + v.Tracking
	}
	v.Preheader += "."
	v.Tag = "Pedido enviado"
	v.ButtonURL, v.ButtonLabel = m.brand.siteURL("/cuenta"), "Ver mis pedidos"
	return m.render(ctx, to, v, shippedHTML, shippedText)
}

func (m *Mailer) SendOrderDelivered(ctx context.Context, to, name string, s sale.Sale) error {
	v := m.orderView(name, s)
	v.TotalLabel = "Total pagado"
	v.Title = "Tu pedido " + v.Reference + " fue entregado"
	v.Preheader = "¡Esperamos que disfrutes tus cartas!"
	v.Tag = "Pedido entregado"
	v.ButtonURL, v.ButtonLabel = m.brand.siteURL("/cartas"), "Seguir comprando"
	return m.render(ctx, to, v, deliveredHTML, deliveredText)
}

// trackingPanel muestra la transportadora y la guía, si se informaron.
const trackingInner = `{{.Delivery}}{{if .Carrier}}<br>Transportadora: <strong style="color:` + cText + `">{{.Carrier}}</strong>{{end}}{{if .Tracking}}<br>Número de guía: <strong style="color:` + cText + `;font-family:` + mono + `">{{.Tracking}}</strong>{{end}}`

var shippedHTML = newHTML(tag(cPrimary) + `
<h1 style="` + sH1 + `">Tu pedido va en camino, {{.Name}}</h1>
<p style="` + sP + `">Despachamos tu pedido <strong style="color:` + cText + `">{{.Reference}}</strong>. Con el número de guía puedes seguirlo en la página de la transportadora.</p>
` + panel(cPrimary, trackingInner) + itemsTable + helpHTML("¿Algún problema con la entrega?") + buttonHTML)

var shippedText = newText(`Tu pedido va en camino, {{.Name}}

Despachamos tu pedido {{.Reference}}. Con el número de guía puedes seguirlo en la página de la transportadora.

{{.Delivery}}{{if .Carrier}}
Transportadora: {{.Carrier}}{{end}}{{if .Tracking}}
Número de guía: {{.Tracking}}{{end}}

` + linesText + `

` + helpText("¿Algún problema con la entrega?") + buttonText)

var deliveredHTML = newHTML(tag(cPrimary) + `
<h1 style="` + sH1 + `">¡Tu pedido llegó, {{.Name}}!</h1>
<p style="` + sP + `">Tu pedido <strong style="color:` + cText + `">{{.Reference}}</strong> fue entregado. Gracias por comprar en Trample Store.</p>
` + itemsTable + helpHTML("¿Algo no llegó bien?") + buttonHTML)

var deliveredText = newText(`¡Tu pedido llegó, {{.Name}}!

Tu pedido {{.Reference}} fue entregado. Gracias por comprar en Trample Store.

` + linesText + `

` + helpText("¿Algo no llegó bien?") + buttonText)
