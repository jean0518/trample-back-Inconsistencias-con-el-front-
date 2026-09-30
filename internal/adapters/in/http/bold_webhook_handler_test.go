package http

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"testing"

	"trample-back/internal/domain/payment"
)

func signBold(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(base64.StdEncoding.EncodeToString(body)))
	return hex.EncodeToString(mac.Sum(nil))
}

func TestVerifyBoldSignature(t *testing.T) {
	body := []byte(`{"type":"SALE_APPROVED"}`)
	sig := signBold(body, "secreta")

	tests := []struct {
		name   string
		header string
		body   []byte
		secret string
		want   bool
	}{
		{name: "firma válida", header: sig, body: body, secret: "secreta", want: true},
		{name: "prefijo sha256= se acepta", header: "sha256=" + sig, body: body, secret: "secreta", want: true},
		{name: "llave distinta", header: sig, body: body, secret: "otra", want: false},
		{name: "body alterado", header: sig, body: []byte(`{"type":"SALE_REJECTED"}`), secret: "secreta", want: false},
		{name: "header vacío", header: "", body: body, secret: "secreta", want: false},
		{name: "hex inválido", header: "zzzz", body: body, secret: "secreta", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := verifyBoldSignature(tt.header, tt.body, tt.secret); got != tt.want {
				t.Fatalf("verifyBoldSignature() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBoldEventStatus(t *testing.T) {
	tests := map[string]string{
		"SALE_APPROVED": payment.StatusApproved,
		"SALE_REJECTED": payment.StatusRejected,
		"VOID_APPROVED": payment.StatusVoided,
		"VOID_REJECTED": payment.StatusVoided,
		"OTRO":          payment.StatusUnknown,
	}
	for eventType, want := range tests {
		if got := boldEventStatus(eventType); got != want {
			t.Errorf("boldEventStatus(%q) = %q, want %q", eventType, got, want)
		}
	}
}

// El evento llega como CloudEvents y la referencia del pedido es la que se envió
// como order-id del botón de pagos.
func TestBoldEventPayload(t *testing.T) {
	raw := []byte(`{
		"id": "0f1d0a2c-notification-uuid",
		"type": "SALE_APPROVED",
		"subject": "tx-999",
		"data": {
			"payment_id": "pay-123",
			"payment_method": "VISA",
			"payer_email": "comprador@ejemplo.com",
			"amount": {"total": 45000, "currency": "COP"},
			"metadata": {"reference": "TRM-1-abcd"}
		}
	}`)

	var ev boldEvent
	if err := DecodeBytes(raw, &ev); err != nil {
		t.Fatalf("decodificar evento: %v", err)
	}

	if ev.ID != "0f1d0a2c-notification-uuid" {
		t.Errorf("id = %q", ev.ID)
	}
	if ev.Type != "SALE_APPROVED" {
		t.Errorf("type = %q", ev.Type)
	}
	if ev.Data.PaymentID != "pay-123" {
		t.Errorf("payment_id = %q", ev.Data.PaymentID)
	}
	if ev.Data.Metadata.Reference != "TRM-1-abcd" {
		t.Errorf("reference = %q, want TRM-1-abcd", ev.Data.Metadata.Reference)
	}
	if ev.Data.Amount.Total != 45000 {
		t.Errorf("amount.total = %d, want 45000", ev.Data.Amount.Total)
	}
	if ev.Data.PayerEmail != "comprador@ejemplo.com" {
		t.Errorf("payer_email = %q", ev.Data.PayerEmail)
	}
}
