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
// abiertos que no correspondan al carrito actual se cierran y liberan su stock.
func (uc *CreateCheckoutUseCase) Execute(ctx context.Context, input sale.ConfirmInput, ids []int64) (CreateCheckoutResult, error) {
	if len(ids) == 0 {
		return CreateCheckoutResult{}, sale.ErrEmptyCart
	}
	if !uc.gateway.Configured() {
		return CreateCheckoutResult{}, payment.ErrNotConfigured
	}
	if err := validateFulfillment(input); err != nil {
		return CreateCheckoutResult{}, err
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

	reuse := err == nil &&
		checkout.ExpiresAt.After(now) &&
		checkout.Fulfillment == input.Fulfillment &&
		checkout.TotalCOP == cart.totalCOP &&
		sameReservations(checkout.ReservationIDs, ids)

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
			checkout, err = uc.newCheckout(ctx, input, cart, ids, now)
			if err != nil {
				return CreateCheckoutResult{}, err
			}
		}
	} else {
		// El carrito cambió (o el checkout anterior venció): los checkouts
		// abiertos se cierran y devuelven su stock antes de crear el nuevo.
		if _, err := uc.sales.CloseOpenCheckouts(ctx, input.UserID); err != nil {
			return CreateCheckoutResult{}, err
		}
		checkout, err = uc.newCheckout(ctx, input, cart, ids, now)
		if err != nil {
			return CreateCheckoutResult{}, err
		}
	}

	return CreateCheckoutResult{
		Checkout: checkout,
		Bold:     uc.gateway.BuildCheckout(uc.checkoutRequest(checkout, input)),
	}, nil
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
// carrito solo aplican antes de pulsar Pagar).
func (uc *CreateCheckoutUseCase) holdStock(ctx context.Context, userID string, ids []int64) error {
	if err := uc.reservations.ExtendExpiry(ctx, userID, ids, uc.holdMinutes); err != nil {
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
