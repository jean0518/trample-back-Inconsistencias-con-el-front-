package http

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strings"

	appCatalog "trample-back/internal/application/catalog"
)

type WebhookHandler struct {
	processWebhook *appCatalog.ProcessWebhookUseCase
	secret         string
}

func NewWebhookHandler(processWebhook *appCatalog.ProcessWebhookUseCase, secret string) *WebhookHandler {
	return &WebhookHandler{processWebhook: processWebhook, secret: secret}
}

// HandleScrydex recibe los eventos de precio de Scrydex, verifica la firma
// HMAC-SHA256 y delega el procesamiento al caso de uso correspondiente.
//
// Formato del header de firma: X-Scrydex-Signature: t=<timestamp>,v1=<hmac>
// Payload firmado: "<timestamp>.<body>"
func (h *WebhookHandler) HandleScrydex(w http.ResponseWriter, r *http.Request) {
	if h.secret == "" {
		Error(w, http.StatusNotImplemented, "webhooks no configurados")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB max
	if err != nil {
		Error(w, http.StatusBadRequest, "no se pudo leer el body")
		return
	}

	if !verifySignature(r.Header.Get("X-Scrydex-Signature"), body, h.secret) {
		Error(w, http.StatusUnauthorized, "firma inválida")
		return
	}

	var event appCatalog.WebhookEvent
	if err := DecodeBytes(body, &event); err != nil {
		Error(w, http.StatusBadRequest, "body JSON inválido")
		return
	}

	// Solo procesamos eventos de precios raw (cartas sin gradear).
	if !strings.HasSuffix(event.Name, ".prices.raw_updated") {
		JSON(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}

	go func() {
		if err := h.processWebhook.Execute(r.Context(), event); err != nil {
			// El error ya quedó loggeado en el use case; aquí solo evitamos
			// que la goroutine muera silenciosamente si lo necesitáramos.
			_ = err
		}
	}()

	JSON(w, http.StatusOK, map[string]string{"status": "accepted"})
}

// verifySignature verifica la firma HMAC-SHA256 del webhook de Scrydex.
// El header tiene el formato "t=<timestamp>,v1=<hex_signature>".
// El payload firmado es "<timestamp>.<body>".
func verifySignature(header string, body []byte, secret string) bool {
	var timestamp, v1 string
	for _, part := range strings.Split(header, ",") {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		switch k {
		case "t":
			timestamp = v
		case "v1":
			v1 = v
		}
	}
	if timestamp == "" || v1 == "" {
		return false
	}

	signed := timestamp + "." + string(body)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signed))
	expected := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(expected), []byte(v1))
}
