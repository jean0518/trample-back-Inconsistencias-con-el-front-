// Package payment modela los conceptos de una pasarela de pagos en línea
// (Bold Colombia): los datos para abrir el checkout, el estado de una
// transacción y las notificaciones que la pasarela envía.
package payment

import (
	"errors"
	"time"
)

// ErrNotConfigured se devuelve cuando falta configuración de la pasarela
// (p. ej. la llave de identidad) y por tanto no se puede cobrar en línea.
var ErrNotConfigured = errors.New("la pasarela de pagos no está configurada")

// ErrAmountTooLow se devuelve cuando el total del pedido está por debajo del
// mínimo que acepta la pasarela (Bold exige al menos 1000 COP).
var ErrAmountTooLow = errors.New("el monto mínimo para pagar en línea es de 1000 COP")

// ProviderBold identifica la pasarela Bold.
const ProviderBold = "bold"

// CurrencyCOP es la única divisa que maneja la tienda.
const CurrencyCOP = "COP"

// Estados normalizados de una transacción, independientes del vocabulario de
// cada pasarela.
const (
	StatusApproved   = "approved"
	StatusRejected   = "rejected"
	StatusFailed     = "failed"
	StatusVoided     = "voided"
	StatusProcessing = "processing"
	StatusPending    = "pending"
	StatusNotFound   = "not_found"
	StatusUnknown    = "unknown"
)

// CheckoutRequest son los datos de la venta que la pasarela necesita. Viaja
// desde el caso de uso hasta el adaptador, que es quien conoce el formato
// exacto que espera el botón de pagos de Bold.
type CheckoutRequest struct {
	Reference      string
	AmountCOP      int64
	Description    string
	RedirectionURL string
	OriginURL      string
	ExpiresAt      time.Time
	PayerEmail     string
	PayerName      string
	PayerPhone     string
	Address        string
	City           string
}

// Checkout agrupa lo que el frontend necesita para renderizar el botón de
// pagos de Bold. La llave de identidad es pública (viaja en el DOM); la firma
// de integridad la calcula el backend con la llave secreta, que nunca sale del
// servidor.
type Checkout struct {
	Reference          string `json:"reference"`
	AmountCOP          int64  `json:"amount_cop"`
	Currency           string `json:"currency"`
	IntegritySignature string `json:"integrity_signature"`
	IdentityKey        string `json:"identity_key"`
	RedirectionURL     string `json:"redirection_url"`
	Description        string `json:"description"`
	// OriginURL es a dónde vuelve el cliente si abandona el pago. Por defecto
	// la pasarela regresa al carrito, que es donde se puede reintentar.
	OriginURL string `json:"origin_url"`
	// ExpirationNS es la expiración en nanosegundos epoch (formato de
	// data-expiration-date). Se alinea con la retención del stock: cuando el
	// pedido se cancela por tiempo, la pasarela también deja de aceptar el pago.
	ExpirationNS int64 `json:"expiration_ns"`
	// CustomerData y BillingAddress son objetos JSON ya serializados que Bold
	// precarga en su formulario para que el comprador no los escriba de nuevo.
	CustomerData   string `json:"customer_data,omitempty"`
	BillingAddress string `json:"billing_address,omitempty"`
}

// Status es el estado de una transacción consultado a la pasarela.
type Status struct {
	Reference     string
	Status        string
	PaymentID     string
	PaymentMethod string
	PayerEmail    string
	AmountCOP     int64
}

// Event es una notificación (webhook) enviada por la pasarela.
type Event struct {
	ID        string
	Type      string
	Reference string
	PaymentID string
	Status    string
	Raw       []byte
}

// Approvable indica si el estado corresponde a un pago aprobado.
func Approvable(status string) bool {
	return status == StatusApproved
}

// Rejected indica si el estado corresponde a un pago rechazado o fallido.
func Rejected(status string) bool {
	return status == StatusRejected || status == StatusFailed
}
