// Package email envía los correos transaccionales de la tienda. Resend es el
// proveedor; sin API key se usa Noop y la tienda sigue funcionando sin avisar.
package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const resendEndpoint = "https://api.resend.com/emails"

// ResendSender manda correos por la API HTTP de Resend.
type ResendSender struct {
	apiKey  string
	from    string
	replyTo string
	client  *http.Client
}

// NewResendSender crea el sender. `replyTo` es opcional: es la bandeja a la que
// llegan las respuestas de los clientes (el remitente es un no-reply).
func NewResendSender(apiKey, from, replyTo string) *ResendSender {
	return &ResendSender{apiKey: apiKey, from: from, replyTo: replyTo, client: &http.Client{Timeout: 10 * time.Second}}
}

type message struct {
	To      string
	Subject string
	HTML    string
	Text    string
}

func (s *ResendSender) send(ctx context.Context, m message) error {
	payload := map[string]any{
		"from":    s.from,
		"to":      []string{m.To},
		"subject": m.Subject,
		"html":    m.HTML,
		"text":    m.Text,
	}
	if s.replyTo != "" {
		payload["reply_to"] = s.replyTo
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, resendEndpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("resend: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("resend: estado %d: %s", resp.StatusCode, detail)
	}
	return nil
}
