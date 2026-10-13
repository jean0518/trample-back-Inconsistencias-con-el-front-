package email

import (
	"context"

	"trample-back/internal/ports/out"
)

var _ out.VerificationMailer = (*Mailer)(nil)

func (m *Mailer) SendEmailVerification(ctx context.Context, to, name, verifyURL string) error {
	v := m.base(name, "", 0)
	v.Title = "Confirma tu correo – Trample Store"
	v.Preheader = "Un clic para activar tu cuenta. El enlace dura 24 horas."
	v.Tag = "Verifica tu correo"
	v.ButtonURL, v.ButtonLabel = verifyURL, "Confirmar mi correo"
	return m.render(ctx, to, v, verifyHTML, verifyText)
}

var verifyHTML = newHTML(tag(cPrimary) + `
<h1 style="` + sH1 + `">Confirma tu correo, {{.Name}}</h1>
<p style="` + sP + `">Gracias por crear tu cuenta en Trample Store. Confirma que este correo es tuyo para recibir los avisos de tus pedidos.</p>
` + buttonHTML + `
` + panel(cPrimary, `El enlace vence en 24 horas. Si no creaste una cuenta en Trample Store, ignora este mensaje: no pasará nada.`) + `
<p style="` + sSmall + `">Si el botón no funciona, copia y pega este enlace en tu navegador:<br><span style="color:` + cSoft + `;word-break:break-all">{{.ButtonURL}}</span></p>`)

var verifyText = newText(`Confirma tu correo, {{.Name}}

Gracias por crear tu cuenta en Trample Store. Confirma que este correo es tuyo para recibir los avisos de tus pedidos:

{{.ButtonURL}}

El enlace vence en 24 horas. Si no creaste una cuenta en Trample Store, ignora este mensaje.`)
