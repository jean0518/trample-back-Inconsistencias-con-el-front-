package sale

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"time"

	"trample-back/internal/domain/reservation"
	"trample-back/internal/domain/sale"
	"trample-back/internal/ports/out"
)

const (
	// defaultHoldMinutes es la ventana de retención del stock mientras el
	// cliente paga en la pasarela. Es configurable (BOLD_PAYMENT_HOLD_MINUTES)
	// porque el carrito usa 5 minutos y esta ventana debe ser generosa: Bold
	// deja el link de pago activo 24 horas.
	defaultHoldMinutes = 30
	// minOnlinePaymentCOP es el monto mínimo que acepta la pasarela.
	minOnlinePaymentCOP = 1000
	// cartPath es la ruta del frontend a la que Bold redirige tras el pago.
	cartPath = "/carrito"
)

// ConfirmResult es el resultado de registrar una venta ya pagada (efectivo o
// transferencia).
type ConfirmResult struct {
	Sale sale.Sale
}

// ConfirmSaleUseCase registra una venta que ya está pagada: efectivo en el acto
// o transferencia confirmada por el administrador.
//
// Los pagos en línea NO pasan por aquí: el cliente paga primero en la pasarela
// y el pedido se crea al aprobarse el pago (ver CreateCheckoutUseCase y
// SaleRepository.ApprovePayment). Es la regla que garantiza que no exista
// ningún pedido sin pagar.
type ConfirmSaleUseCase struct {
	reservations out.ReservationRepository
	sales        out.SaleRepository
}

func NewConfirmSaleUseCase(reservations out.ReservationRepository, sales out.SaleRepository) *ConfirmSaleUseCase {
	return &ConfirmSaleUseCase{reservations: reservations, sales: sales}
}

// Execute confirma las reservas `ids` del carrito del usuario y registra la
// venta.
func (uc *ConfirmSaleUseCase) Execute(ctx context.Context, input sale.ConfirmInput, ids []int64) (ConfirmResult, error) {
	if len(ids) == 0 {
		return ConfirmResult{}, sale.ErrEmptyCart
	}

	// Validaciones de pago y entrega.
	switch input.PaymentMethod {
	case sale.PaymentCash, sale.PaymentTransfer:
		// La opción de efectivo (y la de múltiples métodos) es exclusiva del
		// administrador; el resto de clientes paga en línea o por transferencia.
		if input.PaymentMethod == sale.PaymentCash && !input.IsAdmin {
			return ConfirmResult{}, sale.ErrInvalidInput
		}
	case sale.PaymentBold:
		// El pago con tarjeta se procesa en la pasarela: el pedido se crea al
		// aprobarse el pago, no antes. Quien llama debe usar el checkout.
		return ConfirmResult{}, sale.ErrInvalidInput
	default:
		return ConfirmResult{}, sale.ErrInvalidInput
	}
	if err := validateFulfillment(input); err != nil {
		return ConfirmResult{}, err
	}

	cart, err := priceCart(ctx, uc.reservations, input.UserID, ids, input.Fulfillment)
	if err != nil {
		return ConfirmResult{}, err
	}

	// Confirmamos las reservas primero (marcándolas como 'confirmed') y
	// SOLO después creamos la venta. Si una reserva expiró en el ínterin,
	// Confirm no la afectará; verificamos que se confirmaron todas.
	if err := uc.reservations.Confirm(ctx, input.UserID, ids); err != nil {
		if errors.Is(err, reservation.ErrNotFound) {
			// Una reserva se canceló o expiró justo ahora (carrera):
			// no confirmamos el pedido para no cobrar algo no disponible.
			return ConfirmResult{}, sale.ErrReservationExpired
		}
		return ConfirmResult{}, err
	}

	created, err := uc.sales.Create(ctx, sale.Sale{
		UserID:        input.UserID,
		TotalCOP:      cart.totalCOP,
		TotalUSD:      cart.totalUSD,
		ShippingCOP:   cart.shippingCOP,
		ShippingUSD:   cart.shippingUSD,
		Fulfillment:   input.Fulfillment,
		Address:       input.Address,
		City:          input.City,
		Phone:         input.Phone,
		PaymentMethod: input.PaymentMethod,
		Status:        sale.StatusPaid,
		Items:         cart.items,
	})
	if err != nil {
		return ConfirmResult{}, err
	}
	return ConfirmResult{Sale: created}, nil
}

// validateFulfillment comprueba la forma de entrega y, si es a domicilio, que
// vengan los datos de envío requeridos.
func validateFulfillment(input sale.ConfirmInput) error {
	if input.Fulfillment != sale.FulfillmentPickup && input.Fulfillment != sale.FulfillmentShipping {
		return sale.ErrInvalidInput
	}
	if input.Fulfillment == sale.FulfillmentShipping {
		if input.Address == "" || input.City == "" || input.Phone == "" {
			return sale.ErrInvalidInput
		}
	}
	return nil
}

// pricedCart son los totales y el snapshot de items de un pedido, calculados a
// partir de las reservas activas del carrito.
type pricedCart struct {
	totalCOP    int64
	totalUSD    float64
	shippingCOP int64
	shippingUSD float64
	items       []sale.SaleItem
}

// priceCart convierte las reservas activas del carrito en los totales del
// pedido. Falla si alguna reserva venció o dejó de estar activa, para no cobrar
// algo que ya no está disponible: la validación ocurre ANTES de escribir nada.
func priceCart(
	ctx context.Context,
	reservations out.ReservationRepository,
	userID string,
	ids []int64,
	fulfillment string,
) (pricedCart, error) {
	active, err := reservations.ListActiveByUserAndIDs(ctx, userID, ids)
	if err != nil {
		return pricedCart{}, err
	}
	if len(active) == 0 {
		return pricedCart{}, sale.ErrEmptyCart
	}
	// ListActiveByUserAndIDs filtra por expires_at > now(), así que si devuelve
	// menos de las pedidas es que alguna expiró o ya no está activa.
	if len(active) != len(ids) {
		return pricedCart{}, sale.ErrReservationExpired
	}

	var cart pricedCart
	for _, it := range active {
		cart.totalCOP += int64(it.PriceCOP) * int64(it.Quantity)
		cart.totalUSD += it.PriceUSD * float64(it.Quantity)
		listingID := it.ListingID
		cart.items = append(cart.items, sale.SaleItem{
			ListingID: &listingID,
			CardID:    it.CardID,
			CardName:  it.CardName,
			Language:  it.Language,
			Quantity:  it.Quantity,
			PriceCOP:  int64(it.PriceCOP),
			PriceUSD:  it.PriceUSD,
		})
	}

	// Cargo de envío: solo cuando la entrega es a domicilio. Se suma al total
	// y se guarda por separado (shipping_cop/shipping_usd) para trazabilidad
	// del admin. El equivalente USD se deriva de la mezcla USD/COP del propio
	// pedido (constante para todos los items), sin depender de una red extra.
	if fulfillment == sale.FulfillmentShipping {
		cart.shippingCOP = sale.ShippingCostCOP
		if cart.totalCOP > 0 {
			cart.shippingUSD = math.Round(float64(cart.shippingCOP)*(cart.totalUSD/float64(cart.totalCOP))*100) / 100
		}
		cart.totalCOP += cart.shippingCOP
		cart.totalUSD += cart.shippingUSD
	}
	return cart, nil
}

// newPaymentReference genera el order-id que viaja al botón de pagos: alfanumérico
// con guiones, único (timestamp + aleatorio) y menor de 60 caracteres, como
// exige Bold. Se genera antes de cobrar, pero sin crear ningún pedido: Bold
// identifica la transacción con esta referencia. Nunca se reutiliza una
// referencia cuyo pago ya se liquidó.
func newPaymentReference(now time.Time) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Improbable: el timestamp ya hace la referencia única.
		return fmt.Sprintf("TRM-%d", now.UnixMilli())
	}
	return fmt.Sprintf("TRM-%d-%s", now.UnixMilli(), hex.EncodeToString(b[:]))
}
