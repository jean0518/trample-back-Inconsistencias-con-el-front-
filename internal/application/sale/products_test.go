package sale

import (
	"context"
	"errors"
	"testing"
	"time"

	"trample-back/internal/domain/product"
	"trample-back/internal/domain/reservation"
	"trample-back/internal/domain/sale"
)

// fakeProductReservations devuelve las reservas de producto configuradas y
// registra las extensiones de ventana.
type fakeProductReservations struct {
	list      []product.Reservation
	extended  []extendCall
	extendErr error
}

func (f *fakeProductReservations) Reserve(context.Context, product.ReserveInput, int) (product.Reservation, error) {
	return product.Reservation{}, errors.New("no usado")
}
func (f *fakeProductReservations) UpdateQuantity(context.Context, string, int64, int) (product.Reservation, error) {
	return product.Reservation{}, errors.New("no usado")
}
func (f *fakeProductReservations) ListActiveByUser(context.Context, string) ([]product.Reservation, error) {
	return f.list, nil
}
func (f *fakeProductReservations) ListActiveByUserAndIDs(_ context.Context, userID string, ids []int64) ([]product.Reservation, error) {
	var out []product.Reservation
	for _, r := range f.list {
		if r.UserID != userID {
			continue
		}
		for _, id := range ids {
			if r.ID == id {
				out = append(out, r)
			}
		}
	}
	return out, nil
}
func (f *fakeProductReservations) ExtendExpiry(_ context.Context, userID string, ids []int64, minutes int) error {
	f.extended = append(f.extended, extendCall{userID: userID, ids: ids, minutes: minutes})
	return f.extendErr
}
func (f *fakeProductReservations) Remove(context.Context, string, int64) error   { return nil }
func (f *fakeProductReservations) ReleaseExpired(context.Context) (int64, error) { return 0, nil }

func productReservation(id, productID int64, title string, priceCOP int64, qty int) product.Reservation {
	return product.Reservation{
		ID: id, UserID: testUser, ProductID: productID, Title: title,
		PriceCOP: priceCOP, Quantity: qty, ExpiresAt: time.Now().Add(5 * time.Minute),
	}
}

func newProducts() *fakeProductReservations {
	return &fakeProductReservations{list: []product.Reservation{
		productReservation(10, 100, "Caja de sobres", 450000, 2),
		productReservation(11, 101, "Fundas", 15000, 1),
	}}
}

func TestPriceCart_SoloProductos(t *testing.T) {
	cart, err := priceCart(context.Background(), &fakeReservations{}, newProducts(), testUser,
		nil, []int64{10, 11}, sale.FulfillmentPickup)
	if err != nil {
		t.Fatalf("priceCart: %v", err)
	}
	// 450000*2 + 15000 = 915000, sin envío (recogida) y sin USD.
	if cart.totalCOP != 915000 || cart.totalUSD != 0 {
		t.Errorf("totales COP=%d USD=%v", cart.totalCOP, cart.totalUSD)
	}
	if len(cart.items) != 0 || len(cart.productItems) != 2 {
		t.Fatalf("items=%d productItems=%d", len(cart.items), len(cart.productItems))
	}
	if cart.productItems[0].Title != "Caja de sobres" || *cart.productItems[0].ProductID != 100 {
		t.Errorf("snapshot: %+v", cart.productItems[0])
	}
}

func TestPriceCart_MixtoSumaEnvioUnaVez(t *testing.T) {
	reservations := &fakeReservations{list: []reservation.Reservation{cartReservation(1, testUser, 45000, 11.25, 2)}}
	cart, err := priceCart(context.Background(), reservations, newProducts(), testUser,
		[]int64{1}, []int64{11}, sale.FulfillmentShipping)
	if err != nil {
		t.Fatal(err)
	}
	// 45000*2 + 15000 + 25000 de envío
	if want := int64(90000 + 15000 + sale.ShippingCostCOP); cart.totalCOP != want {
		t.Errorf("totalCOP = %d, se esperaba %d", cart.totalCOP, want)
	}
	if cart.shippingCOP != sale.ShippingCostCOP {
		t.Errorf("envío = %d", cart.shippingCOP)
	}
	if len(cart.items) != 1 || len(cart.productItems) != 1 {
		t.Errorf("items=%d productItems=%d", len(cart.items), len(cart.productItems))
	}
}

func TestPriceCart_ProductoVencidoOSinRepositorio(t *testing.T) {
	// una de las dos reservas ya no está vigente
	_, err := priceCart(context.Background(), &fakeReservations{}, newProducts(), testUser,
		nil, []int64{10, 999}, sale.FulfillmentPickup)
	if !errors.Is(err, sale.ErrReservationExpired) {
		t.Errorf("reserva vencida: %v", err)
	}
	// sin repositorio de productos no se cobra a medias: se rechaza
	_, err = priceCart(context.Background(), &fakeReservations{}, nil, testUser,
		nil, []int64{10}, sale.FulfillmentPickup)
	if !errors.Is(err, sale.ErrInvalidInput) {
		t.Errorf("sin repo de productos: %v", err)
	}
	// carrito vacío de ambas cosas
	_, err = priceCart(context.Background(), &fakeReservations{}, newProducts(), testUser,
		nil, nil, sale.FulfillmentPickup)
	if !errors.Is(err, sale.ErrEmptyCart) {
		t.Errorf("carrito vacío: %v", err)
	}
}

func TestPriceCart_SoloCartasNoCambia(t *testing.T) {
	// regresión: sin productos el resultado es el de siempre y no exige repo de productos
	reservations := &fakeReservations{list: []reservation.Reservation{cartReservation(1, testUser, 45000, 11.25, 2)}}
	cart, err := priceCart(context.Background(), reservations, nil, testUser, []int64{1}, nil, sale.FulfillmentPickup)
	if err != nil || cart.totalCOP != 90000 || len(cart.productItems) != 0 {
		t.Errorf("cart=%+v err=%v", cart, err)
	}
}

func TestConfirmSale_EfectivoSoloProductos(t *testing.T) {
	sales := newFakeSales()
	uc := NewConfirmSaleUseCase(&fakeReservations{}, sales).WithProducts(newProducts())

	result, err := uc.Execute(context.Background(), sale.ConfirmInput{
		UserID: testUser, Fulfillment: sale.FulfillmentPickup, PaymentMethod: sale.PaymentCash,
		IsAdmin: true, ProductReservationIDs: []int64{10, 11},
	}, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Sale.TotalCOP != 915000 || len(result.Sale.ProductItems) != 2 {
		t.Errorf("venta: %+v", result.Sale)
	}
	if len(result.Sale.ProductReservationIDs) != 2 {
		t.Errorf("debía pasar las reservas de producto a Create: %v", result.Sale.ProductReservationIDs)
	}
}

func TestConfirmSale_CarritoVacioConProductosVacios(t *testing.T) {
	uc := NewConfirmSaleUseCase(&fakeReservations{}, newFakeSales()).WithProducts(newProducts())
	_, err := uc.Execute(context.Background(), sale.ConfirmInput{
		UserID: testUser, Fulfillment: sale.FulfillmentPickup, PaymentMethod: sale.PaymentCash, IsAdmin: true,
	}, nil)
	if !errors.Is(err, sale.ErrEmptyCart) {
		t.Errorf("err = %v", err)
	}
}

func TestConfirmSale_ProductoYaNoReservadoSeTraduceAReservaVencida(t *testing.T) {
	sales := newFakeSales()
	sales.confirmErr = product.ErrNotFound // lo que devuelve la confirmación estricta del repo
	uc := NewConfirmSaleUseCase(&fakeReservations{}, sales).WithProducts(newProducts())

	_, err := uc.Execute(context.Background(), sale.ConfirmInput{
		UserID: testUser, Fulfillment: sale.FulfillmentPickup, PaymentMethod: sale.PaymentCash,
		IsAdmin: true, ProductReservationIDs: []int64{10},
	}, nil)
	if !errors.Is(err, sale.ErrReservationExpired) {
		t.Errorf("err = %v", err)
	}
}

func TestCreateCheckout_MixtoRetieneAmbosStocks(t *testing.T) {
	uc, _, sales, gateway := newCheckoutFixture()
	products := newProducts()
	uc.WithProducts(products)

	input := onlineInput()
	input.ProductReservationIDs = []int64{11}
	result, err := uc.Execute(context.Background(), input, []int64{1, 2})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// 45000*2 + 8000 + 15000 + 25000 de envío
	if want := int64(98000 + 15000 + sale.ShippingCostCOP); result.Checkout.TotalCOP != want || result.Bold.AmountCOP != want {
		t.Errorf("total = %d / %d, se esperaba %d", result.Checkout.TotalCOP, result.Bold.AmountCOP, want)
	}
	if len(result.Checkout.ProductItems) != 1 || len(result.Checkout.ProductReservationIDs) != 1 {
		t.Errorf("snapshot de productos: %+v", result.Checkout)
	}
	if len(products.extended) != 1 || products.extended[0].minutes != 30+paymentGraceMinutes {
		t.Errorf("debía extender la reserva del producto a 30+gracia: %+v", products.extended)
	}
	if len(sales.sales) != 0 || len(gateway.requests) != 1 {
		t.Errorf("el checkout no crea venta: ventas=%d", len(sales.sales))
	}
}

func TestCreateCheckout_ReservaDeProductoVencidaAlRetener(t *testing.T) {
	uc, _, _, _ := newCheckoutFixture()
	products := newProducts()
	products.extendErr = product.ErrNotFound
	uc.WithProducts(products)

	input := onlineInput()
	input.ProductReservationIDs = []int64{11}
	if _, err := uc.Execute(context.Background(), input, []int64{1}); !errors.Is(err, sale.ErrReservationExpired) {
		t.Errorf("err = %v", err)
	}
}

func TestCreateCheckout_CambiarProductosNoReutilizaElCheckout(t *testing.T) {
	uc, _, sales, _ := newCheckoutFixture()
	uc.WithProducts(newProducts())

	input := onlineInput()
	input.ProductReservationIDs = []int64{10}
	first, err := uc.Execute(context.Background(), input, []int64{1})
	if err != nil {
		t.Fatal(err)
	}
	// mismo carrito de cartas, otro producto: es otro pedido
	input.ProductReservationIDs = []int64{11}
	second, err := uc.Execute(context.Background(), input, []int64{1})
	if err != nil {
		t.Fatal(err)
	}
	if first.Checkout.Reference == second.Checkout.Reference {
		t.Error("un carrito con otro producto no puede compartir referencia de pago")
	}
	if sales.closeCalls == 0 {
		t.Error("debía cerrar el checkout anterior")
	}
}

func TestCreateCheckout_SoloProductos(t *testing.T) {
	uc, _, _, _ := newCheckoutFixture()
	uc.WithProducts(newProducts())

	input := onlineInput()
	input.ProductReservationIDs = []int64{10}
	result, err := uc.Execute(context.Background(), input, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if want := int64(900000 + sale.ShippingCostCOP); result.Checkout.TotalCOP != want {
		t.Errorf("total = %d, se esperaba %d", result.Checkout.TotalCOP, want)
	}
}

func TestPriceCart_EnvioUSDSoloSeReparteConLasCartas(t *testing.T) {
	reservations := &fakeReservations{list: []reservation.Reservation{cartReservation(1, testUser, 100000, 25, 1)}}

	// mixto: 100.000 COP de cartas (25 USD) + producto de 400.000. El envío en USD
	// sale de la razón de las cartas: 25.000 × 25/100.000 = 6,25, no 1,25.
	cart, err := priceCart(context.Background(), reservations, &fakeProductReservations{list: []product.Reservation{
		productReservation(10, 100, "Caja", 400000, 1),
	}}, testUser, []int64{1}, []int64{10}, sale.FulfillmentShipping)
	if err != nil {
		t.Fatal(err)
	}
	if cart.shippingUSD != 6.25 || cart.totalUSD != 31.25 {
		t.Errorf("shippingUSD=%v totalUSD=%v, se esperaba 6.25 y 31.25", cart.shippingUSD, cart.totalUSD)
	}

	// solo productos: no hay USD que repartir
	cart, err = priceCart(context.Background(), &fakeReservations{}, newProducts(), testUser,
		nil, []int64{10}, sale.FulfillmentShipping)
	if err != nil {
		t.Fatal(err)
	}
	if cart.shippingUSD != 0 || cart.totalUSD != 0 {
		t.Errorf("solo productos debía dar USD 0, fue %v / %v", cart.shippingUSD, cart.totalUSD)
	}
}
