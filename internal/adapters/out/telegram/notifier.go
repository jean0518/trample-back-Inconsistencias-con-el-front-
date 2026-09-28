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

func (n *Notifier) NotifyPriceUpdates(ctx context.Context, gameCode string, changes []catalog.CardPriceChange) error {
	for _, msg := range buildMessages(gameCode, changes) {
		if err := n.send(ctx, msg); err != nil {
			return err
		}
	}
	return nil
}

// buildMessages arma el aviso y lo parte en varios mensajes si no cabe en uno.
// Cada carta va entera en un solo mensaje.
func buildMessages(gameCode string, changes []catalog.CardPriceChange) []string {
	game := gameNames[gameCode]
	if game == "" {
		game = gameCode
	}
	header := fmt.Sprintf("💰 <b>Precios actualizados · %s</b>\n%d %s de tu inventario\n",
		html.EscapeString(game), len(changes), plural(len(changes), "variante", "variantes"))

	var messages []string
	var b strings.Builder
	b.WriteString(header)
	for _, c := range changes {
		entry := formatChange(c)
		if b.Len()+len(entry) > maxMessageLen {
			messages = append(messages, b.String())
			b.Reset()
			b.WriteString("💰 <b>Precios actualizados (cont.)</b>\n")
		}
		b.WriteString(entry)
	}
	return append(messages, b.String())
}

func formatChange(c catalog.CardPriceChange) string {
	u := c.Update
	old := "sin precio"
	if u.HadPrice {
		old = formatUSD(u.OldUSD)
	}
	return fmt.Sprintf("\n• <b>%s</b> (%s) · %s\n  %s → %s · %s COP\n  Español: %s · %d %s\n",
		html.EscapeString(c.CardName),
		html.EscapeString(c.ExpansionID),
		html.EscapeString(u.VariantName),
		old, formatUSD(u.NewUSD), formatCOP(u.NewCOP),
		formatUSD(listing.SpanishPriceUSD(u.NewUSD)),
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
