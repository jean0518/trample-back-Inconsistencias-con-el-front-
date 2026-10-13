package sale

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"trample-back/internal/domain/payment"
	"trample-back/internal/domain/reservation"
	"trample-back/internal/domain/sale"
	"trample-back/internal/ports/out"
)

// CreateCheckoutUseCase prepara el pago en línea con la pasarela.
//
// Es el equivalente de "el cliente pulsó Pagar": valida el pedido, extiende la
// retención del stock y devuelve los datos que el frontend necesita para abrir
// el modal de Bold.
//
// Lo importante es lo que NO hace: no registra ningún pedido. Lo que guarda es
// una intención de pago (sale.CheckoutIntent) identificada por la misma
// referencia que Bold usa como order-id. El pedido se crea únicamente cuando la
// pasarela confirma que el pago entró (SaleRepository.ApprovePayment).
type CreateCheckoutUseCase struct {
	reservations out.ReservationRepository
	sales        out.SaleRepository
	gateway      out.PaymentGateway
	holdMinutes  int
	frontendURL  string
}

func NewCreateCheckoutUseCase(
	reservations out.ReservationRepository,
	sales out.SaleRepository,
	gateway out.PaymentGateway,
	holdMinutes int,
	frontendURL string,
) *CreateCheckoutUseCase {
	if holdMinutes <= 0 {
		holdMinutes = defaultHoldMinutes
	}
	return &CreateCheckoutUseCase{
		reservations: reservations,
		sales:        sales,
		gateway:      gateway,
		holdMinutes:  holdMinutes,
		frontendURL:  frontendURL,
	}
}

// CreateCheckoutResult son los datos que el frontend necesita para abrir la
// pasarela: la referencia del checkout (para poder consultarlo después) y los
// parámetros del botón de pagos.
type CreateCheckoutResult struct {
	Checkout sale.CheckoutIntent
	Bold     payment.Checkout
}

// Execute deja listo el pago de las reservas `ids` del carrito de `input`.
//
// Si el cliente ya tenía un checkout abierto para ese mismo carrito (abrió el
// modal, lo cerró y volvió a pulsar Pagar), se devuelve ese mismo con la misma
// referencia: así Bold no ve dos pedidos para la misma compra. Los checkouts
// abiertos que no correspondan al carrito actual se cierran, conservando las
// reservas del carrito.
func (uc *CreateCheckoutUseCase) Execute(ctx context.Context, input sale.ConfirmInput, ids []int64) (CreateCheckoutResult, error) {
	if len(ids) == 0 {
		return CreateCheckoutResult{}, sale.ErrEmptyCart
	}
	if !uc.gateway.Configured() {
		return CreateCheckoutResult{}, payment.ErrNotConfigured
	}
	input = normalizeFulfillment(input)
	if err := validateFulfillment(input); err != nil {
		return CreateCheckoutResult{}, err
	}
	// La recogida en tienda se paga en el local (ver PaymentStore): no hay nada
	// que cobrar en línea, y aceptarla aquí dejaría pagar con tarjeta un pedido
	// que debe apartarse sin pago.
	if input.Fulfillment == sale.FulfillmentPickup {
		return CreateCheckoutResult{}, sale.ErrInvalidInput
	}

	cart, err := priceCart(ctx, uc.reservations, input.UserID, ids, input.Fulfillment)
	if err != nil {
		return CreateCheckoutResult{}, err
	}
	if cart.totalCOP < minOnlinePaymentCOP {
		return CreateCheckoutResult{}, payment.ErrAmountTooLow
	}

	now := time.Now()
	checkout, err := uc.sales.FindOpenCheckoutByUser(ctx, input.UserID)
	if err != nil && !errors.Is(err, sale.ErrPaymentNotFound) {
		return CreateCheckoutResult{}, err
	}

	// matches indica si un checkout abierto corresponde a este mismo carrito.
	matches := func(c sale.CheckoutIntent) bool {
		return c.ExpiresAt.After(now) &&
			c.Fulfillment == input.Fulfillment &&
			c.TotalCOP == cart.totalCOP &&
			sameReservations(c.ReservationIDs, ids)
	}
	reuse := err == nil && matches(checkout)

	if reuse {
		// Se renueva la ventana completa: el stock queda retenido y el checkout
		// sigue vivo durante el mismo tiempo. Ambas cosas van juntas porque el
		// barrido usa `expires_at` para decidir a qué checkout liberarle las
		// cartas; si solo se moviera el stock, podría soltarlo mientras el
		// cliente sigue pagando.
		if err := uc.holdStock(ctx, input.UserID, ids); err != nil {
			return CreateCheckoutResult{}, err
		}
		expiresAt := now.Add(time.Duration(uc.holdMinutes) * time.Minute)
		checkout, err = uc.sales.ExtendCheckout(ctx, checkout.Reference, expiresAt)
		if err != nil {
			// El checkout se cerró entre la lectura y ahora; se vuelve a crear.
			checkout, err = uc.createOrJoin(ctx, input, cart, ids, now, matches)
			if err != nil {
				return CreateCheckoutResult{}, err
			}
		}
	} else {
		// El carrito cambió (o el checkout anterior venció): los checkouts
		// abiertos se cierran antes de crear el nuevo. Sus reservas no se
		// liberan, porque son las mismas que sostienen el carrito actual.
		if _, err := uc.sales.CloseOpenCheckouts(ctx, input.UserID); err != nil {
			return CreateCheckoutResult{}, err
		}
		checkout, err = uc.createOrJoin(ctx, input, cart, ids, now, matches)
		if err != nil {
			return CreateCheckoutResult{}, err
		}
	}

	return CreateCheckoutResult{
		Checkout: checkout,
		Bold:     uc.gateway.BuildCheckout(uc.checkoutRequest(checkout, input)),
	}, nil
}

// createOrJoin crea el checkout nuevo. Si otro "Pagar" simultáneo del mismo
// usuario ganó la carrera (el índice único permite un solo checkout abierto por
// usuario), se une a ese en vez de fallar, siempre que corresponda al mismo
// carrito; si no, devuelve sale.ErrCheckoutConflict para que el cliente reintente.
func (uc *CreateCheckoutUseCase) createOrJoin(
	ctx context.Context,
	input sale.ConfirmInput,
	cart pricedCart,
	ids []int64,
	now time.Time,
	matches func(sale.CheckoutIntent) bool,
) (sale.CheckoutIntent, error) {
	checkout, err := uc.newCheckout(ctx, input, cart, ids, now)
	if !errors.Is(err, sale.ErrCheckoutConflict) {
		return checkout, err
	}
	existing, findErr := uc.sales.FindOpenCheckoutByUser(ctx, input.UserID)
	if findErr == nil && matches(existing) {
		return existing, nil
	}
	return sale.CheckoutIntent{}, sale.ErrCheckoutConflict
}

// newCheckout retiene el stock y guarda una intención de pago nueva. El orden
// importa: primero se aparta el inventario y después se registra el checkout,
// para no dejar filas huérfanas si la reserva falla.
func (uc *CreateCheckoutUseCase) newCheckout(
	ctx context.Context,
	input sale.ConfirmInput,
	cart pricedCart,
	ids []int64,
	now time.Time,
) (sale.CheckoutIntent, error) {
	if err := uc.holdStock(ctx, input.UserID, ids); err != nil {
		return sale.CheckoutIntent{}, err
	}

	intent := sale.CheckoutIntent{
		Reference:      newPaymentReference(now),
		UserID:         input.UserID,
		TotalCOP:       cart.totalCOP,
		TotalUSD:       cart.totalUSD,
		ShippingCOP:    cart.shippingCOP,
		ShippingUSD:    cart.shippingUSD,
		Fulfillment:    input.Fulfillment,
		Address:        input.Address,
		City:           input.City,
		Phone:          input.Phone,
		PaymentMethod:  sale.PaymentBold,
		Items:          cart.items,
		ReservationIDs: ids,
		ExpiresAt:      now.Add(time.Duration(uc.holdMinutes) * time.Minute),
	}
	return uc.sales.CreateCheckout(ctx, intent)
}

// holdStock extiende la ventana de las reservas del carrito para que el stock
// quede retenido mientras el cliente paga en la pasarela (los 5 minutos del
// carrito solo aplican antes de pulsar Pagar). Dura `paymentGraceMinutes` más
// que el vencimiento que ve la pasarela, para no soltar el stock de un pago que
// se aprueba justo al límite.
func (uc *CreateCheckoutUseCase) holdStock(ctx context.Context, userID string, ids []int64) error {
	if err := uc.reservations.ExtendExpiry(ctx, userID, ids, uc.holdMinutes+paymentGraceMinutes); err != nil {
		if errors.Is(err, reservation.ErrNotFound) {
			return sale.ErrReservationExpired
		}
		return err
	}
	return nil
}

// checkoutRequest arma los datos que viaja a la pasarela. La firma de integridad
// la calcula el adaptador con la llave secreta, que nunca sale del backend.
func (uc *CreateCheckoutUseCase) checkoutRequest(c sale.CheckoutIntent, input sale.ConfirmInput) payment.CheckoutRequest {
	redirection := uc.redirectionURL()
	return payment.CheckoutRequest{
		Reference:      c.Reference,
		AmountCOP:      c.TotalCOP,
		Description:    "Pedido " + c.Reference,
		RedirectionURL: redirection,
		// Si el cliente abandona en la pasarela, vuelve al carrito, que es
		// donde puede reintentar el pago.
		OriginURL:  redirection,
		ExpiresAt:  c.ExpiresAt,
		PayerEmail: input.PayerEmail,
		PayerName:  input.PayerName,
		PayerPhone: c.Phone,
		Address:    c.Address,
		City:       c.City,
	}
}

// redirectionURL es a dónde vuelve el cliente tras pagar: el carrito del
// frontend, con la referencia del checkout para poder validar el pago.
func (uc *CreateCheckoutUseCase) redirectionURL() string {
	return strings.TrimRight(uc.frontendURL, "/") + cartPath
}

// sameReservations compara dos conjuntos de reservas de carrito sin importar el
// orden en que llegaron.
func sameReservations(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	x := slices.Clone(a)
	y := slices.Clone(b)
	slices.Sort(x)
	slices.Sort(y)
	return slices.Equal(x, y)
}
