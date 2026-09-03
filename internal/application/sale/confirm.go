package sale

import (
	"context"
	"errors"
	"math"

	"trample-back/internal/domain/reservation"
	"trample-back/internal/domain/sale"
	"trample-back/internal/ports/out"
)

// ConfirmSaleUseCase registra una venta local confirmando las reservas del
// carrito. No se conecta con ninguna pasarela externa (Bold) todavía.
type ConfirmSaleUseCase struct {
	reservations out.ReservationRepository
	sales        out.SaleRepository
}

func NewConfirmSaleUseCase(reservations out.ReservationRepository, sales out.SaleRepository) *ConfirmSaleUseCase {
	return &ConfirmSaleUseCase{reservations: reservations, sales: sales}
}

// Execute confirma las reservas `ids` del carrito del usuario y registra la
// venta de forma local.
func (uc *ConfirmSaleUseCase) Execute(ctx context.Context, input sale.ConfirmInput, ids []int64) (sale.Sale, error) {
	if len(ids) == 0 {
		return sale.Sale{}, sale.ErrEmptyCart
	}

	// Validaciones de pago y entrega.
	if input.PaymentMethod != sale.PaymentCash && input.PaymentMethod != sale.PaymentTransfer {
		return sale.Sale{}, sale.ErrInvalidInput
	}
	if input.PaymentMethod == sale.PaymentCash && !input.IsAdmin {
		// La opción de efectivo (y la de múltiples métodos) es exclusiva del
		// administrador; el resto de clientes paga por transferencia.
		return sale.Sale{}, sale.ErrInvalidInput
	}
	if input.Fulfillment != sale.FulfillmentPickup && input.Fulfillment != sale.FulfillmentShipping {
		return sale.Sale{}, sale.ErrInvalidInput
	}
	// Para envío se requieren dirección, ciudad y teléfono (campos activados).
	if input.Fulfillment == sale.FulfillmentShipping {
		if input.Address == "" || input.City == "" || input.Phone == "" {
			return sale.Sale{}, sale.ErrInvalidInput
		}
	}

	items, err := uc.reservations.ListActiveByUserAndIDs(ctx, input.UserID, ids)
	if err != nil {
		return sale.Sale{}, err
	}
	if len(items) == 0 {
		return sale.Sale{}, sale.ErrEmptyCart
	}
	// Si alguna reserva expiró o ya no está activa, ListActiveByUserAndIDs
	// (que filtra por expires_at > now()) no la devolverá. En ese caso
	// abortamos ANTES de persistir la venta para no cobrar algo que ya no
	// está disponible.
	if len(items) != len(ids) {
		return sale.Sale{}, sale.ErrReservationExpired
	}

	var (
		totalCOP  int64
		totalUSD  float64
		saleItems []sale.SaleItem
	)
	for _, it := range items {
		totalCOP += int64(float64(it.PriceCOP)) * int64(it.Quantity)
		totalUSD += it.PriceUSD * float64(it.Quantity)
		listingID := it.ListingID
		saleItems = append(saleItems, sale.SaleItem{
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
	var shippingCOP int64
	var shippingUSD float64
	if input.Fulfillment == sale.FulfillmentShipping {
		shippingCOP = sale.ShippingCostCOP
		if totalCOP > 0 {
			shippingUSD = math.Round(float64(shippingCOP)*(totalUSD/float64(totalCOP))*100) / 100
		}
		totalCOP += shippingCOP
		totalUSD += shippingUSD
	}

	// Confirmamos las reservas primero (marcándolas como 'confirmed') y
	// SOLO después creamos la venta. Si una reserva expiró en el ínterin,
	// Confirm no la afectará; verificamos que se confirmaron todas.
	if err := uc.reservations.Confirm(ctx, input.UserID, ids); err != nil {
		if errors.Is(err, reservation.ErrNotFound) {
			// Una reserva se canceló o expiró justo ahora (carrera):
			// no confirmamos el pedido para no cobrar algo no disponible.
			return sale.Sale{}, sale.ErrReservationExpired
		}
		return sale.Sale{}, err
	}

	created, err := uc.sales.Create(ctx, sale.Sale{
		UserID:        input.UserID,
		TotalCOP:      totalCOP,
		TotalUSD:      totalUSD,
		ShippingCOP:   shippingCOP,
		ShippingUSD:   shippingUSD,
		Fulfillment:   input.Fulfillment,
		Address:       input.Address,
		City:          input.City,
		Phone:         input.Phone,
		PaymentMethod: input.PaymentMethod,
		Status:        sale.StatusPaid,
		Items:         saleItems,
	})
	if err != nil {
		return sale.Sale{}, err
	}

	return created, nil
}
