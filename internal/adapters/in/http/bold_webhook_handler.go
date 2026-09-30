package http

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"strings"

	appSale "trample-back/internal/application/sale"
	"trample-back/internal/domain/payment"
)

// BoldWebhookHandler recibe las notificaciones de la pasarela de pagos Bold.
// La ruta es pública (la pasarela no manda token) y la autenticidad se
// garantiza con la firma HMAC del body.
type BoldWebhookHandler struct {
	process *appSale.ProcessPaymentEventUseCase
	secret  string
	// isTest cambia la forma de verificar la firma: en el ambiente de pruebas
	// Bold firma los eventos con la llave secreta VACÍA, aunque en el panel sí
	// exista una llave secreta de pruebas (esa solo se usa para la firma de
	// integridad del checkout).
	isTest bool
}

func NewBoldWebhookHandler(process *appSale.ProcessPaymentEventUseCase, secret string, isTest bool) *BoldWebhookHandler {
	return &BoldWebhookHandler{process: process, secret: secret, isTest: isTest}
}

// boldEvent es el sobre CloudEvents que envía Bold. La referencia del pedido es
// la que nosotros enviamos como order-id del botón de pagos (data.metadata.reference).
type boldEvent struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Data struct {
		PaymentID string `json:"payment_id"`
		Amount    struct {
			Total    int64  `json:"total"`
			Currency string `json:"currency"`
		} `json:"amount"`
		Metadata struct {
			Reference string `json:"reference"`
		} `json:"metadata"`
		PaymentMethod string `json:"payment_method"`
		PayerEmail    string `json:"payer_email"`
	} `json:"data"`
}

// HandleBold procesa una notificación de pago. Responde 200 de inmediato
// (Bold exige hacerlo en menos de 2 segundos y reintenta si no) y aplica el
// cambio de estado en segundo plano.
//
//	@Summary      Webhook de pagos Bold
//	@Tags         payments
//	@Accept       json
//	@Produce      json
//	@Param        body  body      object  true  "Notificación de Bold"
//	@Success      200   {object}  object{status=string}
//	@Failure      400   {object}  object{error=string}
//	@Failure      401   {object}  object{error=string}
//	@Router       /webhooks/bold [post]
func (h *BoldWebhookHandler) HandleBold(w http.ResponseWriter, r *http.Request) {
	// La firma se calcula con la llave secreta del ambiente. En pruebas es una
	// cadena vacía; en producción tiene que existir, y sin ella el webhook
	// quedaría abierto (cualquiera podría marcar ventas como pagadas).
	signingKey := h.secret
	if h.isTest {
		signingKey = ""
	} else if signingKey == "" {
		Error(w, http.StatusNotImplemented, "webhooks de pagos no configurados")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB max
	if err != nil {
		Error(w, http.StatusBadRequest, "no se pudo leer el body")
		return
	}

	if !verifyBoldSignature(r.Header.Get("x-bold-signature"), body, signingKey) {
		Error(w, http.StatusUnauthorized, "firma inválida")
		return
	}

	var ev boldEvent
	if err := DecodeBytes(body, &ev); err != nil {
		Error(w, http.StatusBadRequest, "body JSON inválido")
		return
	}
	if ev.Type == "" {
		Error(w, http.StatusBadRequest, "evento sin tipo")
		return
	}

	evt := payment.Event{
		ID:        ev.ID,
		Type:      ev.Type,
		Reference: ev.Data.Metadata.Reference,
		PaymentID: ev.Data.PaymentID,
		Status:    boldEventStatus(ev.Type),
		Raw:       body,
	}

	bgCtx := context.WithoutCancel(r.Context())
	go func() {
		if _, err := h.process.Execute(bgCtx, evt); err != nil {
			slog.Error("procesar notificación de pago",
				slog.String("type", evt.Type),
				slog.String("reference", evt.Reference),
				slog.Any("error", err))
		}
	}()

	JSON(w, http.StatusOK, map[string]string{"status": "accepted"})
}

// verifyBoldSignature valida el header x-bold-signature: es el digest en hex de
// HMAC-SHA256 de la llave secreta sobre el body codificado en base64.
func verifyBoldSignature(header string, body []byte, secret string) bool {
	header = strings.TrimSpace(header)
	// Algunas integraciones prefijan el algoritmo; se acepta ambas formas.
	header = strings.TrimSpace(strings.TrimPrefix(header, "sha256="))
	if header == "" {
		return false
	}
	received, err := hex.DecodeString(header)
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(base64.StdEncoding.EncodeToString(body)))
	return hmac.Equal(received, mac.Sum(nil))
}

// boldEventStatus traduce el tipo de evento de Bold al vocabulario del dominio.
func boldEventStatus(eventType string) string {
	switch eventType {
	case appSale.EventSaleApproved:
		return payment.StatusApproved
	case appSale.EventSaleRejected:
		return payment.StatusRejected
	case appSale.EventVoidApproved, appSale.EventVoidRejected:
		// Una anulación deja de ser un pago válido: el stock debe volver a
		// estar disponible.
		return payment.StatusVoided
	default:
		return payment.StatusUnknown
	}
}
