package out

import (
	"context"

	"trample-back/internal/domain/payment"
)

// PaymentGateway abstrae la pasarela de pagos en línea (Bold). El dominio no
// conoce el protocolo de Bold; solo pide datos de checkout y estados.
type PaymentGateway interface {
	// Configured indica si hay llaves cargadas y por tanto se puede cobrar en
	// línea. Sin esto, el checkout con Bold debe rechazarse con un error claro.
	Configured() bool
	// BuildCheckout arma los parámetros del botón de pagos, incluida la firma
	// de integridad calculada con la llave secreta.
	BuildCheckout(req payment.CheckoutRequest) payment.Checkout
	// GetStatus consulta el estado de una transacción por su referencia
	// (order-id). Es la vía para validar el pago cuando el webhook no llega,
	// como ocurre en el ambiente de pruebas de Bold.
	GetStatus(ctx context.Context, reference string) (payment.Status, error)
}
