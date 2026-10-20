package email

import (
	"context"
	"fmt"

	"trample-back/internal/domain/sale"
	"trample-back/internal/ports/out"
)

// paymentLabels traduce el método de pago guardado a lo que lee el staff.
var paymentLabels = map[string]string{
	sale.PaymentBold:     "En línea (Bold)",
	sale.PaymentCash:     "Efectivo",
	sale.PaymentTransfer: "Transferencia",
	sale.PaymentStore:    "En tienda, al recoger",
}

// adminView agrega al view común los datos que solo ve el staff.
type adminView struct {
	view
	BuyerEmail     string
	Phone          string
	Payment        string
	PaymentRef     string
	Pending        bool // recogida apartada, aún sin pagar
	RequiresReview bool
}

// adminBase arma la vista común de los correos al staff.
func (m *Mailer) adminBase(buyer out.OrderBuyer, s sale.Sale) adminView {
	v := adminView{
		view:           m.orderView(buyer.Name, s),
		BuyerEmail:     buyer.Email,
		Phone:          s.Phone,
		Payment:        firstOr(paymentLabels[s.PaymentMethod], s.PaymentMethod),
		PaymentRef:     s.PaymentReference,
		Pending:        s.Status == sale.StatusPendingPickup,
		RequiresReview: s.RequiresReview,
	}
	if v.Pending && !s.PickupDeadline.IsZero() {
		v.Deadline = formatDeadline(s.PickupDeadline)
	}
	v.TotalLabel = "Total"
	v.ButtonURL, v.ButtonLabel = m.brand.siteURL("/admin/pedidos"), "Ver pedidos"
	return v
}

func (m *Mailer) SendAdminNewOrder(ctx context.Context, to string, buyer out.OrderBuyer, s sale.Sale) error {
	v := m.adminBase(buyer, s)
	if v.Pending {
		v.TotalLabel = "Total a cobrar en tienda"
		v.Tag = "Pedido apartado"
		v.Title = "Pedido apartado " + v.Reference + " – " + v.Total
	} else {
		v.TotalLabel = "Total pagado"
		v.Tag = "Nueva venta"
		v.Title = "Nueva venta " + v.Reference + " – " + v.Total
	}
	if v.RequiresReview {
		v.Title = "[REVISAR] " + v.Title
	}
	v.Preheader = v.Name + " · " + v.Total + " · " + v.Delivery
	return m.renderData(ctx, to, v.Title, v, adminOrderHTML, adminOrderText)
}

// adminAlertView agrega el tipo de alerta a la vista del staff.
type adminAlertView struct {
	adminView
	CancelRequested bool
	PickupExpired   bool
	PaymentVoided   bool
}

func (m *Mailer) SendAdminAlert(ctx context.Context, to string, buyer out.OrderBuyer, s sale.Sale, alert out.AdminAlert) error {
	v := adminAlertView{adminView: m.adminBase(buyer, s)}
	switch alert {
	case out.AlertCancelRequested:
		v.CancelRequested = true
		v.Tag = "Cancelación solicitada"
		v.Title = "El cliente pidió cancelar el pedido " + v.Reference
		v.Preheader = v.Name + " pidió cancelar su pedido apartado. Apruébalo o recházalo en el panel."
	case out.AlertPickupExpired:
		v.PickupExpired = true
		v.Tag = "Pedido vencido"
		v.Title = "Pedido " + v.Reference + " vencido sin recoger"
		v.Preheader = "Se canceló y el stock volvió al inventario."
	case out.AlertPaymentVoided:
		v.PaymentVoided = true
		v.Tag = "Pago anulado"
		v.Title = "⚠️ NO DESPACHAR – pago anulado del pedido " + v.Reference
		v.Preheader = "La pasarela anuló el pago de un pedido ya creado. Revísalo antes de despachar."
	default:
		return fmt.Errorf("aviso al staff desconocido: %d", alert)
	}
	return m.renderData(ctx, to, v.Title, v, adminAlertHTML, adminAlertText)
}

var adminOrderHTML = newHTML(tag(cPrimary) + `
<h1 style="` + sH1 + `">{{if .Pending}}Pedido apartado{{else}}Nueva venta{{end}} {{.Reference}}</h1>
{{if .RequiresReview}}` + panel(cDanger, `<strong style="color:`+cText+`">Requiere revisión manual.</strong> El pago llegó tarde (el stock ya se había liberado) o con un monto distinto. Confirma que el inventario está completo y luego marca el pedido como revisado en el panel: ahí le llega la confirmación al cliente. No despaches antes.`) + `{{end}}
` + adminContactHTML + `
` + itemsTable + panel(cPrimary, `{{.Delivery}}{{if .Deadline}}<br>Debe recogerse antes del {{.Deadline}}.{{end}}`) + buttonHTML)

var adminOrderText = newText(`{{if .Pending}}Pedido apartado{{else}}Nueva venta{{end}} {{.Reference}}
{{if .RequiresReview}}
REQUIERE REVISIÓN MANUAL: el pago llegó tarde (el stock ya se había liberado) o con un monto distinto. Confirma que el inventario está completo y luego marca el pedido como revisado en el panel: ahí le llega la confirmación al cliente. No despaches antes.
{{end}}
` + adminContactText + `

` + linesText + `

{{.Delivery}}{{if .Deadline}}
Debe recogerse antes del {{.Deadline}}.{{end}}` + buttonText)

// adminContactHTML y adminContactText son el bloque de contacto del comprador.
const adminContactHTML = `<p style="` + sP + `"><strong style="color:` + cText + `">{{.Name}}</strong>{{if .BuyerEmail}} · <a href="mailto:{{.BuyerEmail}}" style="color:` + cPrimary + `;text-decoration:none">{{.BuyerEmail}}</a>{{end}}{{if .Phone}} · {{.Phone}}{{end}}<br>
Pago: {{.Payment}}{{if .PaymentRef}} <span style="color:` + cMuted + `">({{.PaymentRef}})</span>{{end}}</p>`

const adminContactText = `Cliente: {{.Name}}{{if .BuyerEmail}} <{{.BuyerEmail}}>{{end}}{{if .Phone}}
Teléfono: {{.Phone}}{{end}}
Pago: {{.Payment}}{{if .PaymentRef}} ({{.PaymentRef}}){{end}}`

var adminAlertHTML = newHTML(`{{if .CancelRequested}}` + tag(cPrimary) + `{{else}}` + tag(cDanger) + `{{end}}
{{if .CancelRequested}}<h1 style="` + sH1 + `">Solicitud de cancelación {{.Reference}}</h1>
` + panel(cPrimary, `El cliente pidió cancelar este pedido apartado. <strong style="color:`+cText+`">Apruébalo o recházalo en el panel de pedidos</strong>; mientras tanto el stock sigue apartado.{{if .Deadline}}<br>Si nadie responde, vence el {{.Deadline}}.{{end}}`) + `
{{else if .PickupExpired}}<h1 style="` + sH1 + `">Pedido {{.Reference}} vencido</h1>
` + panel(cDanger, `No se recogió dentro del plazo. El pedido se canceló, el stock volvió al inventario y se le avisó al cliente.`) + `
{{else}}<h1 style="` + sH1 + `">Pago anulado – pedido {{.Reference}}</h1>
` + panel(cDanger, `<strong style="color:`+cText+`">No despaches este pedido.</strong> La pasarela anuló el pago después de aprobarlo. El pedido quedó marcado para revisión; revisa con Bold si hay que reembolsar y contacta al cliente.`) + `
{{end}}` + adminContactHTML + itemsTable + buttonHTML)

var adminAlertText = newText(`{{if .CancelRequested}}Solicitud de cancelación {{.Reference}}

El cliente pidió cancelar este pedido apartado. Apruébalo o recházalo en el panel de pedidos; mientras tanto el stock sigue apartado.{{if .Deadline}}
Si nadie responde, vence el {{.Deadline}}.{{end}}{{else if .PickupExpired}}Pedido {{.Reference}} vencido

No se recogió dentro del plazo. El pedido se canceló, el stock volvió al inventario y se le avisó al cliente.{{else}}Pago anulado – pedido {{.Reference}}

No despaches este pedido. La pasarela anuló el pago después de aprobarlo. El pedido quedó marcado para revisión; revisa con Bold si hay que reembolsar y contacta al cliente.{{end}}

` + adminContactText + `

` + linesText + buttonText)
