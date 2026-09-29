// Package telegram envía avisos al staff por un bot de Telegram.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"trample-back/internal/domain/catalog"
	"trample-back/internal/domain/listing"
	"trample-back/internal/ports/out"
)

const apiURL = "https://api.telegram.org/bot%s/sendMessage"

// maxMessageLen deja margen bajo el límite de 4096 caracteres de Telegram.
const maxMessageLen = 3800

var gameNames = map[string]string{
	"pokemon":   "Pokémon",
	"mtg":       "Magic",
	"riftbound": "Riftbound",
}

type Notifier struct {
	http   *http.Client
	token  string
	chatID string
}

// NewNotifier devuelve nil si falta el token o el chat, para que el webhook
// siga funcionando sin Telegram configurado.
func NewNotifier(token, chatID string) out.PriceUpdateNotifier {
	if token == "" || chatID == "" {
		return nil
	}
	return &Notifier{
		http:   &http.Client{Timeout: 10 * time.Second},
		token:  token,
		chatID: chatID,
	}
}

func (n *Notifier) NotifyPriceReport(ctx context.Context, report catalog.PriceReport) error {
	for _, msg := range buildMessages(report) {
		if err := n.send(ctx, msg); err != nil {
			return err
		}
	}
	return nil
}

// skipOrder fija el orden en que se listan los motivos en el mensaje.
var skipOrder = []string{
	catalog.SkipNotInDB,
	catalog.SkipNoStock,
	catalog.SkipScrydexError,
	catalog.SkipDBError,
}

// buildMessages arma el aviso y lo parte en varios mensajes si no cabe en uno.
// Cada renglón va entero en un solo mensaje.
func buildMessages(report catalog.PriceReport) []string {
	game := gameNames[report.GameCode]
	if game == "" {
		game = report.GameCode
	}

	var entries []string
	if len(report.Changes) > 0 {
		entries = append(entries, fmt.Sprintf("💰 <b>Precios actualizados · %s</b>\n%d %s de tu inventario\n",
			html.EscapeString(game), len(report.Changes), plural(len(report.Changes), "variante", "variantes")))
		for _, c := range report.Changes {
			entries = append(entries, formatChange(report.GameCode, c))
		}
	} else {
		entries = append(entries, fmt.Sprintf("📭 <b>Evento de precios · %s</b>\nNingún precio de tu inventario cambió.\n",
			html.EscapeString(game)))
	}

	if len(report.Skipped) > 0 {
		byReason := make(map[string][]catalog.SkippedExpansion)
		for _, sk := range report.Skipped {
			byReason[sk.Reason] = append(byReason[sk.Reason], sk)
		}
		entries = append(entries, fmt.Sprintf("\n⏭️ <b>Expansiones no procesadas (%d)</b>\n", len(report.Skipped)))
		for _, reason := range skipOrder {
			group := byReason[reason]
			if len(group) == 0 {
				continue
			}
			entries = append(entries, fmt.Sprintf("<i>%s (%d)</i>\n", html.EscapeString(capitalize(reason)), len(group)))
			for _, sk := range group {
				entries = append(entries, formatSkipped(sk))
			}
		}
	}

	var messages []string
	var b strings.Builder
	for _, entry := range entries {
		if b.Len() > 0 && b.Len()+len(entry) > maxMessageLen {
			messages = append(messages, b.String())
			b.Reset()
			b.WriteString("<i>(continuación)</i>\n")
		}
		b.WriteString(entry)
	}
	return append(messages, b.String())
}

func formatSkipped(sk catalog.SkippedExpansion) string {
	if sk.Name != "" {
		return fmt.Sprintf("• %s (<code>%s</code>)\n", html.EscapeString(sk.Name), html.EscapeString(sk.ID))
	}
	return fmt.Sprintf("• <code>%s</code>\n", html.EscapeString(sk.ID))
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func formatChange(gameCode string, c catalog.CardPriceChange) string {
	u := c.Update
	old := "sin precio"
	if u.HadPrice {
		old = formatUSD(u.OldUSD)
	}
	// Solo el español de Pokémon se publica al 80 %; en cualquier otro idioma el
	// precio de venta es el de mercado, así que la línea sería engañosa.
	spanish := ""
	if listing.IsDiscountedLanguage(gameCode, "Español") {
		spanish = fmt.Sprintf("\n  Español: %s", formatUSD(listing.SpanishPriceUSD(u.NewUSD)))
	}
	return fmt.Sprintf("\n• <b>%s</b> (%s) · %s\n  %s → %s · %s COP%s\n  %d %s\n",
		html.EscapeString(c.CardName),
		html.EscapeString(c.ExpansionID),
		html.EscapeString(u.VariantName),
		old, formatUSD(u.NewUSD), formatCOP(u.NewCOP),
		spanish,
		u.ListingsRepriced, plural(int(u.ListingsRepriced), "listing", "listings"),
	)
}

func (n *Notifier) send(ctx context.Context, text string) error {
	body, err := json.Marshal(map[string]any{
		"chat_id":                  n.chatID,
		"text":                     text,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf(apiURL, n.token), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.http.Do(req)
	if err != nil {
		// El error de red puede incluir la URL con el token: no se propaga.
		return fmt.Errorf("telegram: no se pudo enviar el mensaje")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("telegram: status %d: %s", resp.StatusCode, msg)
	}
	return nil
}

func formatUSD(v float64) string {
	return "US$" + strconv.FormatFloat(v, 'f', 2, 64)
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

func plural(n int, singular, pluralForm string) string {
	if n == 1 {
		return singular
	}
	return pluralForm
}
