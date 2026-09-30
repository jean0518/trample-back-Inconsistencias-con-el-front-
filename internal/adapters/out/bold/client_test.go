package bold

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"trample-back/internal/domain/payment"
)

func TestBuildCheckout(t *testing.T) {
	c := NewClient("llave-publica", "llave-secreta", "https://payments.api.bold.co")

	expires := time.Date(2026, 3, 1, 12, 30, 0, 0, time.UTC)
	checkout := c.BuildCheckout(payment.CheckoutRequest{
		Reference:      "TRM-1-abcd",
		AmountCOP:      45000,
		Description:    "Pedido TRM-1-abcd",
		RedirectionURL: "http://localhost:5173/carrito",
		OriginURL:      "http://localhost:5173/carrito",
		ExpiresAt:      expires,
		PayerEmail:     "comprador@ejemplo.com",
		PayerName:      "Ana Rivera",
		PayerPhone:     "300 123 4567",
		Address:        "Calle 123 #45-67",
		City:           "Bogotá",
	})

	// La firma es el SHA256 de {referencia}{monto}{divisa}{llave secreta}. Si este
	// valor cambia, Bold rechaza el botón.
	sum := sha256.Sum256([]byte("TRM-1-abcd" + "45000" + "COP" + "llave-secreta"))
	want := hex.EncodeToString(sum[:])

	if checkout.IntegritySignature != want {
		t.Fatalf("integrity signature = %q, want %q", checkout.IntegritySignature, want)
	}
	if checkout.IdentityKey != "llave-publica" {
		t.Fatalf("identity key = %q, want %q", checkout.IdentityKey, "llave-publica")
	}
	if checkout.Currency != payment.CurrencyCOP {
		t.Fatalf("currency = %q, want %q", checkout.Currency, payment.CurrencyCOP)
	}
	if checkout.AmountCOP != 45000 {
		t.Fatalf("amount = %d, want 45000", checkout.AmountCOP)
	}
	// Bold espera nanosegundos epoch en data-expiration-date, no segundos.
	if checkout.ExpirationNS != expires.UnixNano() {
		t.Fatalf("expiration = %d, want %d", checkout.ExpirationNS, expires.UnixNano())
	}
}

func TestBuildCheckoutPrefill(t *testing.T) {
	c := NewClient("llave-publica", "llave-secreta", "")

	checkout := c.BuildCheckout(payment.CheckoutRequest{
		Reference:  "TRM-2-efgh",
		AmountCOP:  45000,
		PayerEmail: "comprador@ejemplo.com",
		PayerName:  "Ana Rivera",
		PayerPhone: "300 123 4567",
		Address:    "Calle 123 #45-67",
		City:       "Bogotá",
	})

	var customer map[string]string
	if err := json.Unmarshal([]byte(checkout.CustomerData), &customer); err != nil {
		t.Fatalf("customer data no es JSON: %v", err)
	}
	if customer["email"] != "comprador@ejemplo.com" {
		t.Errorf("email = %q", customer["email"])
	}
	if customer["fullName"] != "Ana Rivera" {
		t.Errorf("fullName = %q", customer["fullName"])
	}
	// El teléfono va sin formato y con el código de país aparte.
	if customer["phone"] != "3001234567" {
		t.Errorf("phone = %q, want 3001234567", customer["phone"])
	}
	if customer["dialCode"] != "+57" {
		t.Errorf("dialCode = %q, want +57", customer["dialCode"])
	}

	var billing map[string]string
	if err := json.Unmarshal([]byte(checkout.BillingAddress), &billing); err != nil {
		t.Fatalf("billing address no es JSON: %v", err)
	}
	if billing["address"] != "Calle 123 #45-67" || billing["city"] != "Bogotá" {
		t.Errorf("billing = %v", billing)
	}
}

func TestBuildCheckoutSinDatosDelComprador(t *testing.T) {
	c := NewClient("llave-publica", "llave-secreta", "")

	// Sin datos del comprador no se mandan objetos vacíos: Bold los trata como
	// opcionales y un "{}" no aporta nada.
	checkout := c.BuildCheckout(payment.CheckoutRequest{Reference: "TRM-3", AmountCOP: 5000})
	if checkout.CustomerData != "" || checkout.BillingAddress != "" {
		t.Fatalf("no debía prellenar datos: %q / %q", checkout.CustomerData, checkout.BillingAddress)
	}
}

func TestConfigured(t *testing.T) {
	if NewClient("", "", "").Configured() {
		t.Fatal("sin llave de identidad no debe estar configurado")
	}
	if !NewClient("llave", "", "").Configured() {
		t.Fatal("con llave de identidad debe estar configurado")
	}
}

func TestGetStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "x-api-key llave-publica" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.URL.Path; got != "/v2/payment-voucher/TRM-1-abcd" {
			t.Errorf("path = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"reference_id": "TRM-1-abcd",
			"payment_status": "APPROVED",
			"transaction_id": "tx-999",
			"payment_method": "VISA",
			"payer_email": "comprador@ejemplo.com",
			"total": 45000
		}`))
	}))
	defer srv.Close()

	c := NewClient("llave-publica", "", srv.URL)
	status, err := c.GetStatus(context.Background(), "TRM-1-abcd")
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}

	if status.Status != payment.StatusApproved {
		t.Errorf("status = %q, want %q", status.Status, payment.StatusApproved)
	}
	if status.PaymentID != "tx-999" {
		t.Errorf("payment id = %q, want tx-999", status.PaymentID)
	}
	if status.AmountCOP != 45000 {
		t.Errorf("amount = %d, want 45000", status.AmountCOP)
	}
}

func TestNormalizeStatus(t *testing.T) {
	tests := map[string]string{
		"APPROVED":             payment.StatusApproved,
		"approved":             payment.StatusApproved,
		" REJECTED ":           payment.StatusRejected,
		"FAILED":               payment.StatusFailed,
		"VOIDED":               payment.StatusVoided,
		"PROCESSING":           payment.StatusProcessing,
		"PENDING":              payment.StatusPending,
		"NO_TRANSACTION_FOUND": payment.StatusNotFound,
		"":                     payment.StatusUnknown,
		"ALGO_RARO":            payment.StatusUnknown,
	}
	for raw, want := range tests {
		if got := normalizeStatus(raw); got != want {
			t.Errorf("normalizeStatus(%q) = %q, want %q", raw, got, want)
		}
	}
}
