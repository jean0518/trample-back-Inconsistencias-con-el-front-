package email

import (
	"html/template"
	"strings"
	texttemplate "text/template"
)

// Brand son los datos de la tienda que enmarcan todos los correos: logo, enlaces
// y el pie. Salen de la configuración, así que cambiarlos no toca las plantillas.
type Brand struct {
	SiteURL      string // https://tramplestore.com (sin barra final)
	LogoURL      string // PNG público: Gmail y Outlook no muestran SVG
	ContactEmail string
	Location     string
	Social       string // @tramplestore
}

// Paleta del sitio (globals.css, tema oscuro) en hex: los clientes de correo no
// entienden hsl() ni variables CSS.
const (
	cBg      = "#151723" // --background
	cCard    = "#1b1e2d" // --card
	cPanel   = "#222639" // --secondary
	cBorder  = "#262b40" // --border
	cText    = "#f1f1f4" // --foreground
	cSoft    = "#d6d8e1"
	cMuted   = "#818598" // --muted-foreground
	cPrimary = "#a2df3a" // --primary
	cOnPrim  = "#191c29" // --primary-foreground
	cDanger  = "#d4372b" // --destructive

	font = `'Geist',-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif`
	mono = `'Geist Mono',ui-monospace,SFMono-Regular,Menlo,Consolas,monospace`
)

// Estilos en línea compartidos: el correo no puede depender de una hoja de estilos.
const (
	sH1    = `margin:0 0 16px;font-family:` + font + `;font-size:24px;line-height:1.25;font-weight:700;color:` + cText
	sP     = `margin:0 0 16px;font-family:` + font + `;font-size:15px;line-height:1.6;color:` + cSoft
	sSmall = `margin:0;font-family:` + font + `;font-size:13px;line-height:1.6;color:` + cMuted
	sTd    = `padding:10px 0;border-bottom:1px solid ` + cBorder + `;font-family:` + font + `;font-size:14px;line-height:1.4;color:` + cSoft
)

// tag es la etiqueta de color sobre el título ("PAGO CONFIRMADO").
func tag(color string) string {
	return `<p style="margin:0 0 12px;font-family:` + mono + `;font-size:11px;letter-spacing:2px;font-weight:700;text-transform:uppercase;color:` + color + `">{{.Tag}}</p>`
}

const buttonHTML = `{{if .ButtonURL}}<table role="presentation" cellpadding="0" cellspacing="0" border="0" style="margin:8px 0 8px"><tr><td bgcolor="` + cPrimary + `" style="border-radius:10px"><a href="{{.ButtonURL}}" style="display:inline-block;padding:14px 28px;font-family:` + font + `;font-size:15px;font-weight:700;color:` + cOnPrim + `;text-decoration:none;border-radius:10px">{{.ButtonLabel}}</a></td></tr></table>{{end}}`

// itemsTable lista las cartas del pedido y el total.
const itemsTable = `<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="margin:8px 0 20px">
{{range .Lines}}<tr><td style="` + sTd + `">{{.Quantity}} × {{.Name}}{{if .Language}} <span style="color:` + cMuted + `">· {{.Language}}</span>{{end}}</td><td align="right" style="` + sTd + `;white-space:nowrap">{{.Subtotal}}</td></tr>
{{end}}{{if .Shipping}}<tr><td style="` + sTd + `">Envío a domicilio</td><td align="right" style="` + sTd + `;white-space:nowrap">{{.Shipping}}</td></tr>
{{end}}<tr><td style="padding:14px 0 0;font-family:` + font + `;font-size:15px;font-weight:700;color:` + cText + `">{{.TotalLabel}}</td><td align="right" style="padding:14px 0 0;font-family:` + font + `;font-size:20px;font-weight:700;color:` + cPrimary + `;white-space:nowrap">{{.Total}}</td></tr>
</table>`

// panel es un recuadro destacado; `accent` es el color de su franja izquierda.
func panel(accent, inner string) string {
	return `<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="margin:0 0 20px"><tr><td bgcolor="` + cPanel + `" style="padding:16px 18px;border-left:4px solid ` + accent + `;border-radius:8px;font-family:` + font + `;font-size:14px;line-height:1.6;color:` + cSoft + `">` + inner + `</td></tr></table>`
}

const layoutHTML = `<!doctype html>
<html lang="es"><head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="color-scheme" content="dark light">
<meta name="supported-color-schemes" content="dark light">
<title>{{.Title}}</title>
</head>
<body style="margin:0;padding:0;background:` + cBg + `">
<div style="display:none;max-height:0;overflow:hidden;opacity:0;color:` + cBg + `">{{.Preheader}}</div>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" bgcolor="` + cBg + `" style="background:` + cBg + `"><tr><td align="center" style="padding:28px 12px">
<table role="presentation" width="600" cellpadding="0" cellspacing="0" border="0" style="width:100%;max-width:600px">
<tr><td align="center" style="padding:4px 0 24px">
{{if .Brand.LogoURL}}<a href="{{.Brand.SiteURL}}" style="text-decoration:none"><img src="{{.Brand.LogoURL}}" width="96" alt="Trample Store" style="display:block;margin:0 auto 10px;border:0;height:auto"></a>{{end}}
<div style="font-family:` + mono + `;font-size:13px;font-weight:700;letter-spacing:4px;color:` + cText + `">TRAMPLE<span style="color:` + cPrimary + `">STORE</span></div>
</td></tr>
<tr><td bgcolor="` + cCard + `" style="background:` + cCard + `;border:1px solid ` + cBorder + `;border-radius:16px;padding:32px 28px">
{{template "content" .}}
</td></tr>
<tr><td align="center" style="padding:24px 8px 0">
<p style="` + sSmall + `">{{if .Brand.Social}}{{.Brand.Social}}{{end}}{{if and .Brand.Social .Brand.Location}} · {{end}}{{.Brand.Location}}</p>
{{if .Brand.ContactEmail}}<p style="` + sSmall + `;margin-top:4px">¿Dudas? Escríbenos a <a href="mailto:{{.Brand.ContactEmail}}" style="color:` + cPrimary + `;text-decoration:none">{{.Brand.ContactEmail}}</a></p>{{end}}
<p style="` + sSmall + `;margin-top:12px;font-size:12px">© {{.Year}} Trample Store · Mensaje automático sobre tu compra.</p>
</td></tr>
</table>
</td></tr></table>
</body></html>`

const layoutText = `{{template "content" .}}

--
Trample Store{{if .Brand.Location}} · {{.Brand.Location}}{{end}}{{if .Brand.ContactEmail}}
¿Dudas? Escríbenos a {{.Brand.ContactEmail}}{{end}}
`

// newHTML y newText arman un correo completo: el layout común más el contenido
// propio. Las plantillas son constantes del programa: si una no compila es un bug
// que debe aparecer al arrancar, de ahí Must.
func newHTML(content string) *template.Template {
	t := template.Must(template.New("email").Parse(layoutHTML))
	template.Must(t.New("content").Parse(content))
	return t // se ejecuta "email" (el layout), que incluye "content"
}

func newText(content string) *texttemplate.Template {
	t := texttemplate.Must(texttemplate.New("email").Parse(layoutText))
	texttemplate.Must(t.New("content").Parse(content))
	return t
}

// siteURL arma un enlace dentro del sitio; vacío si no hay SiteURL configurado
// (el correo sale sin botón en vez de con un enlace roto).
func (b Brand) siteURL(path string) string {
	if b.SiteURL == "" {
		return ""
	}
	return strings.TrimRight(b.SiteURL, "/") + path
}
