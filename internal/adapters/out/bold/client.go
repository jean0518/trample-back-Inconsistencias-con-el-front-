// Package bold implementa el puerto PaymentGateway contra la pasarela de
// pagos en línea de Bold Colombia. Cubre el Botón de pagos (manual) y la
// consulta activa del estado de una transacción.
package bold

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"trample-back/internal/domain/payment"
)

// Client habla con Bold. La llave de identidad es pública; la secreta nunca
// sale del backend y solo se usa para calcular la firma de integridad del
// checkout.
type Client struct {
	identityKey string
	secretKey   string
	baseURL     string
	http        *http.Client
}

func NewClient(identityKey, secretKey, baseURL string) *Client {
	return &Client{
		identityKey: identityKey,
		secretKey:   secretKey,
		baseURL:     strings.TrimRight(baseURL, "/"),
		http:        &http.Client{Timeout: 15 * time.Second},
	}
}

// Configured indica si hay llave de identidad: sin ella no se puede cobrar en
// línea.
func (c *Client) Configured() bool {
	return c.identityKey != ""
}

// BuildCheckout arma los parámetros del botón de pagos. La firma de integridad
// es SHA256 de la concatenación `{referencia}{monto}{divisa}{llave secreta}`,
// tal como exige Bold; se calcula aquí, en el servidor, para no exponer la
// llave secreta en el navegador.
func (c *Client) BuildCheckout(req payment.CheckoutRequest) payment.Checkout {
	raw := req.Reference + strconv.FormatInt(req.AmountCOP, 10) + payment.CurrencyCOP + c.secretKey
	sum := sha256.Sum256([]byte(raw))
	return payment.Checkout{
		Reference:          req.Reference,
		AmountCOP:          req.AmountCOP,
		Currency:           payment.CurrencyCOP,
		IntegritySignature: hex.EncodeToString(sum[:]),
		IdentityKey:        c.identityKey,
		RedirectionURL:     req.RedirectionURL,
		Description:        req.Description,
		OriginURL:          req.OriginURL,
		// Bold espera nanosegundos desde la época Unix, no segundos.
		ExpirationNS:   req.ExpiresAt.UnixNano(),
		CustomerData:   customerDataJSON(req),
		BillingAddress: billingAddressJSON(req),
	}
}

// customerDataJSON prellena los datos del comprador en el formulario de Bold.
// Bold espera un objeto convertido a string y admite campos opcionales, así que
// solo se incluyen los que realmente tenemos.
func customerDataJSON(req payment.CheckoutRequest) string {
	data := map[string]string{}
	if req.PayerEmail != "" {
		data["email"] = req.PayerEmail
	}
	if req.PayerName != "" {
		data["fullName"] = req.PayerName
	}
	// Bold espera el teléfono sin formato junto al código de país.
	if digits := onlyDigits(req.PayerPhone); len(digits) > 6 {
		data["phone"] = digits
		data["dialCode"] = "+57"
	}
	return marshalObject(data)
}

// billingAddressJSON prellena la dirección de envío.
func billingAddressJSON(req payment.CheckoutRequest) string {
	data := map[string]string{}
	if req.Address != "" {
		data["address"] = req.Address
	}
	if req.City != "" {
		data["city"] = req.City
	}
	return marshalObject(data)
}

func marshalObject(data map[string]string) string {
	if len(data) == 0 {
		return ""
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return ""
	}
	return string(raw)
}

func onlyDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

type voucherResponse struct {
	ReferenceID   string `json:"reference_id"`
	PaymentStatus string `json:"payment_status"`
	TransactionID string `json:"transaction_id"`
	PaymentMethod string `json:"payment_method"`
	PayerEmail    string `json:"payer_email"`
	Total         int64  `json:"total"`
}

// GetStatus consulta el estado de una transacción por su referencia. Es la vía
// para validar el pago cuando el webhook no llega (en el ambiente de pruebas
// de Bold no se envían webhooks automáticamente).
func (c *Client) GetStatus(ctx context.Context, reference string) (payment.Status, error) {
	if !c.Configured() {
		return payment.Status{}, payment.ErrNotConfigured
	}

	endpoint := c.baseURL + "/v2/payment-voucher/" + url.PathEscape(reference)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return payment.Status{}, fmt.Errorf("bold: armar consulta: %w", err)
	}
	req.Header.Set("Authorization", "x-api-key "+c.identityKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return payment.Status{}, fmt.Errorf("bold: consultar estado: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusUnauthorized {
		return payment.Status{}, fmt.Errorf("bold: llave de identidad inválida (401)")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return payment.Status{}, fmt.Errorf("bold: consulta falló con %d", resp.StatusCode)
	}

	var v voucherResponse
	if err := json.Unmarshal(body, &v); err != nil {
		return payment.Status{}, fmt.Errorf("bold: respuesta inválida: %w", err)
	}

	status := payment.Status{
		Reference:     v.ReferenceID,
		Status:        normalizeStatus(v.PaymentStatus),
		PaymentID:     v.TransactionID,
		PaymentMethod: v.PaymentMethod,
		PayerEmail:    v.PayerEmail,
		AmountCOP:     v.Total,
	}
	if status.Reference == "" {
		status.Reference = reference
	}
	return status, nil
}

// normalizeStatus traduce los estados de Bold a los del dominio.
func normalizeStatus(raw string) string {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "APPROVED":
		return payment.StatusApproved
	case "REJECTED":
		return payment.StatusRejected
	case "FAILED":
		return payment.StatusFailed
	case "VOIDED":
		return payment.StatusVoided
	case "PROCESSING":
		return payment.StatusProcessing
	case "PENDING":
		return payment.StatusPending
	case "NO_TRANSACTION_FOUND":
		return payment.StatusNotFound
	case "":
		return payment.StatusUnknown
	default:
		return payment.StatusUnknown
	}
}
