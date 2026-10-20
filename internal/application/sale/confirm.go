package sale

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"trample-back/internal/domain/product"
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
	// paymentGraceMinutes es el margen que se agrega a la retención del stock más
	// allá del vencimiento que ve la pasarela. Bold deja de aceptar pagos nuevos al
	// vencer, pero uno que ya estaba en curso (PSE, transferencia bancaria) puede
	// aprobarse minutos después: sin margen, el stock ya se habría liberado.
	paymentGraceMinutes = 5
	// minOnlinePaymentCOP es el monto mínimo que acepta la pasarela.
	minOnlinePaymentCOP = 1000
	// cartPath es la ruta del frontend a la que Bold redirige tras el pago.
	cartPath = "/carrito"
)

// paymentGrace es paymentGraceMinutes como duración.
const paymentGrace = paymentGraceMinutes * time.Minute

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
	products     out.ProductReservationRepository
	sales        out.SaleRepository
	notifier     *PurchaseNotifier
	users        userFinder
}

// WithEmailVerification exige que el cliente haya verificado su correo para
// apartar un pedido (el staff queda exento).
func (uc *ConfirmSaleUseCase) WithEmailVerification(users userFinder) *ConfirmSaleUseCase {
	uc.users = users
	return uc
}

// WithProducts activa la venta de productos propios en el mismo pedido que las
// cartas. Sin esto, un carrito con reservas de productos se rechaza.
func (uc *ConfirmSaleUseCase) WithProducts(p out.ProductReservationRepository) *ConfirmSaleUseCase {
	uc.products = p
	return uc
}

// WithNotifier activa el correo de "pedido apartado" de la recogida en tienda y
// el aviso al staff de cada venta registrada.
func (uc *ConfirmSaleUseCase) WithNotifier(n *PurchaseNotifier) *ConfirmSaleUseCase {
	uc.notifier = n
	return uc
}

func NewConfirmSaleUseCase(reservations out.ReservationRepository, sales out.SaleRepository) *ConfirmSaleUseCase {
	return &ConfirmSaleUseCase{reservations: reservations, sales: sales}
}

// Execute confirma las reservas `ids` del carrito del usuario y registra la
// venta.
func (uc *ConfirmSaleUseCase) Execute(ctx context.Context, input sale.ConfirmInput, ids []int64) (ConfirmResult, error) {
	if len(ids) == 0 && len(input.ProductReservationIDs) == 0 {
		return ConfirmResult{}, sale.ErrEmptyCart
	}

	input = normalizeFulfillment(input)

	// Validaciones de pago y entrega.
	switch input.PaymentMethod {
	case sale.PaymentCash, sale.PaymentTransfer:
		// Efectivo y transferencia registran una venta ya pagada, así que solo el
		// staff puede hacerlo: es quien comprueba que el dinero entró. Un cliente
		// que pudiera elegir "transferencia" obtendría un pedido pagado (y el
		// inventario descontado) sin pagar; el cliente paga en línea por el checkout.
		if !input.IsAdmin {
			return ConfirmResult{}, sale.ErrInvalidInput
		}
	case sale.PaymentStore:
		// La recogida en tienda se paga en el local: cualquier cliente puede
		// apartar el pedido, pero solo con entrega en tienda. El pedido nace
		// pendiente (stock apartado) y vence a los 2 días hábiles; el staff lo
		// marca como pagado cuando el cliente paga y lo recoge.
		if input.Fulfillment != sale.FulfillmentPickup {
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
	if err := requireVerifiedEmail(ctx, uc.users, input); err != nil {
		return ConfirmResult{}, err
	}

	cart, err := priceCart(ctx, uc.reservations, uc.products, input.UserID, ids, input.ProductReservationIDs, input.Fulfillment)
	if err != nil {
		return ConfirmResult{}, err
	}

	// Las reservas se confirman dentro de la misma transacción que registra la
	// venta (Create). Así, si algo falla, no queda un carrito confirmado sin pedido
	// ni un pedido sin las reservas confirmadas.
	status, deadline := sale.StatusPaid, time.Time{}
	if input.PaymentMethod == sale.PaymentStore {
		status, deadline = sale.StatusPendingPickup, sale.PickupDeadlineFrom(time.Now())
	}

	created, err := uc.sales.Create(ctx, sale.Sale{
		UserID:         input.UserID,
		TotalCOP:       cart.totalCOP,
		TotalUSD:       cart.totalUSD,
		ShippingCOP:    cart.shippingCOP,
		ShippingUSD:    cart.shippingUSD,
		Fulfillment:    input.Fulfillment,
		Address:        input.Address,
		City:           input.City,
		Phone:          input.Phone,
		PaymentMethod:  input.PaymentMethod,
		Status:         status,
		PickupDeadline: deadline,
		Items:          cart.items,
		ReservationIDs: ids,

		ProductItems:          cart.productItems,
		ProductReservationIDs: input.ProductReservationIDs,
	})
	if err != nil {
		if errors.Is(err, reservation.ErrNotFound) || errors.Is(err, product.ErrNotFound) {
			// Una reserva se canceló o expiró justo ahora (carrera): no se
			// confirma el pedido para no registrar algo no disponible.
			return ConfirmResult{}, sale.ErrReservationExpired
		}
		return ConfirmResult{}, err
	}
	if created.Status == sale.StatusPendingPickup {
		uc.notifier.PickupReserved(created)
	} else {
		uc.notifier.AdminNewOrder(created)
	}
	return ConfirmResult{Sale: created}, nil
}

// requireVerifiedEmail rechaza la compra de un cliente con el correo sin
// verificar: los avisos del pedido (confirmación, cancelación, envío) solo le
// llegan a un correo válido. El staff registra ventas a nombre propio y queda
// exento. Sin `users` (verificación desactivada) no se exige nada.
func requireVerifiedEmail(ctx context.Context, users userFinder, input sale.ConfirmInput) error {
	if users == nil || input.IsAdmin {
		return nil
	}
	u, err := users.FindByID(ctx, input.UserID)
	if err != nil {
		return err
	}
	if !u.EmailVerified {
		return sale.ErrEmailNotVerified
	}
	return nil
}

// validateFulfillment comprueba la forma de entrega y, si es a domicilio, que
// vengan los datos de envío requeridos.
//
// Espera la entrada ya normalizada (normalizeFulfillment): sin espacios sobrantes,
// para que "   " no cuente como una dirección.
func validateFulfillment(input sale.ConfirmInput) error {
	if input.Fulfillment != sale.FulfillmentPickup && input.Fulfillment != sale.FulfillmentShipping {
		return sale.ErrInvalidInput
	}
	if input.Fulfillment == sale.FulfillmentShipping {
		if input.Address == "" || input.City == "" || input.Phone == "" {
			return sale.ErrInvalidInput
		}
		if len(input.Address) > maxAddressLen || len(input.City) > maxCityLen {
			return sale.ErrInvalidInput
		}
		// Un teléfono sin dígitos suficientes no sirve para coordinar la entrega.
		if n := len(onlyDigits(input.Phone)); n < minPhoneDigits || n > maxPhoneDigits {
			return sale.ErrInvalidInput
		}
	}
	return nil
}

const (
	maxAddressLen  = 200
	maxCityLen     = 80
	minPhoneDigits = 7
	maxPhoneDigits = 15
)

// normalizeFulfillment recorta los espacios de los datos de entrega y del
// comprador. Debe aplicarse antes de validar y de guardar el pedido.
func normalizeFulfillment(in sale.ConfirmInput) sale.ConfirmInput {
	in.Address = strings.TrimSpace(in.Address)
	in.City = strings.TrimSpace(in.City)
	in.Phone = strings.TrimSpace(in.Phone)
	in.PayerName = strings.TrimSpace(in.PayerName)
	return in
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

// pricedCart son los totales y el snapshot de items de un pedido, calculados a
// partir de las reservas activas del carrito.
type pricedCart struct {
	totalCOP    int64
	totalUSD    float64
	shippingCOP int64
	shippingUSD float64
	items       []sale.SaleItem
	// productItems son las líneas de productos propios de la tienda. Solo tienen
	// precio en COP, por eso no suman al totalUSD.
	productItems []sale.ProductItem
}

// priceCart convierte las reservas activas del carrito en los totales del
// pedido. Falla si alguna reserva venció o dejó de estar activa, para no cobrar
// algo que ya no está disponible: la validación ocurre ANTES de escribir nada.
func priceCart(
	ctx context.Context,
	reservations out.ReservationRepository,
	products out.ProductReservationRepository,
	userID string,
	ids []int64,
	productIDs []int64,
	fulfillment string,
) (pricedCart, error) {
	if len(ids) == 0 && len(productIDs) == 0 {
		return pricedCart{}, sale.ErrEmptyCart
	}

	var cart pricedCart
	// cardsCOP es lo que suman solo las cartas: único tramo del pedido que tiene
	// equivalente en USD, y por tanto la base para repartir el envío en USD.
	var cardsCOP int64
	if len(ids) > 0 {
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

		for _, it := range active {
			// Se redondea al peso (no se trunca): un precio de 12345.9 se cobra como
			// 12346, no como 12345.
			priceCOP := int64(math.Round(it.PriceCOP))
			cart.totalCOP += priceCOP * int64(it.Quantity)
			cardsCOP += priceCOP * int64(it.Quantity)
			cart.totalUSD += it.PriceUSD * float64(it.Quantity)
			listingID := it.ListingID
			cart.items = append(cart.items, sale.SaleItem{
				ListingID: &listingID,
				CardID:    it.CardID,
				CardName:  it.CardName,
				Language:  it.Language,
				// La variante dice qué copia despachar; se guarda como snapshot.
				VariantName: it.VariantName,
				Quantity:    it.Quantity,
				PriceCOP:    priceCOP,
				PriceUSD:    it.PriceUSD,
			})
		}
	}

	if len(productIDs) > 0 {
		// Sin el repositorio de productos conectado no se puede cobrar un carrito
		// con productos: se rechaza en vez de ignorarlos y cobrar solo las cartas.
		if products == nil {
			return pricedCart{}, sale.ErrInvalidInput
		}
		active, err := products.ListActiveByUserAndIDs(ctx, userID, productIDs)
		if err != nil {
			return pricedCart{}, err
		}
		if len(active) != len(productIDs) {
			return pricedCart{}, sale.ErrReservationExpired
		}
		for _, it := range active {
			cart.totalCOP += it.PriceCOP * int64(it.Quantity)
			productID := it.ProductID
			cart.productItems = append(cart.productItems, sale.ProductItem{
				ProductID: &productID,
				Title:     it.Title,
				Quantity:  it.Quantity,
				PriceCOP:  it.PriceCOP,
			})
		}
	}

	// Cargo de envío: solo cuando la entrega es a domicilio. Se suma al total
	// y se guarda por separado (shipping_cop/shipping_usd) para trazabilidad
	// del admin. El equivalente USD se deriva de la mezcla USD/COP del propio
	// pedido (constante para todos los items), sin depender de una red extra.
	if fulfillment == sale.FulfillmentShipping {
		cart.shippingCOP = sale.ShippingCostCOP
		// Se reparte con la razón USD/COP de las cartas; los productos no tienen
		// USD y diluirían la razón (en un pedido solo de productos no hay USD).
		if cardsCOP > 0 {
			cart.shippingUSD = math.Round(float64(cart.shippingCOP)*(cart.totalUSD/float64(cardsCOP))*100) / 100
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
