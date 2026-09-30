package sale

import (
	"context"
	"errors"
	"testing"
	"time"

	"trample-back/internal/domain/payment"
	"trample-back/internal/domain/reservation"
	"trample-back/internal/domain/sale"
)

const (
	testUser  = "42"
	testFront = "https://tienda.test"
)

func pickupInput() sale.ConfirmInput {
	return sale.ConfirmInput{
		UserID:        testUser,
		Fulfillment:   sale.FulfillmentPickup,
		PaymentMethod: sale.PaymentBold,
		PayerEmail:    "cliente@test.com",
		PayerName:     "Cliente Test",
	}
}

func newCheckoutFixture() (*CreateCheckoutUseCase, *fakeReservations, *fakeSales, *fakeGateway) {
	reservations := &fakeReservations{
		list: []reservation.Reservation{
			cartReservation(1, testUser, 45000, 11.25, 2),
			cartReservation(2, testUser, 8000, 2, 1),
		},
	}
	sales := newFakeSales()
	gateway := &fakeGateway{configured: true}
	uc := NewCreateCheckoutUseCase(reservations, sales, gateway, 30, testFront)
	return uc, reservations, sales, gateway
}

// La regla central del cambio: abrir el checkout de la pasarela NO debe crear
// ninguna venta. El pedido se crea al aprobarse el pago.
func TestCreateCheckout_NoRegistraPedido(t *testing.T) {
	uc, _, sales, _ := newCheckoutFixture()

	result, err := uc.Execute(context.Background(), pickupInput(), []int64{1, 2})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(sales.sales) != 0 {
		t.Fatalf("se registraron %d ventas; el checkout no debe crear ninguna", len(sales.sales))
	}
	if sales.createCalls != 1 {
		t.Fatalf("CreateCheckout llamado %d veces, se esperaba 1", sales.createCalls)
	}
	if result.Checkout.Status != sale.CheckoutOpen {
		t.Errorf("status = %q, se esperaba %q", result.Checkout.Status, sale.CheckoutOpen)
	}
	if result.Checkout.Reference == "" {
		t.Error("la referencia del checkout viene vacía; Bold la necesita como order-id")
	}
}

func TestCreateCheckout_CalculaTotales(t *testing.T) {
	uc, _, _, gateway := newCheckoutFixture()

	result, err := uc.Execute(context.Background(), pickupInput(), []int64{1, 2})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// 45000*2 + 8000*1 = 98000, sin envío porque es recogida en tienda.
	const wantCOP = int64(98000)
	const wantUSD = 24.5
	if result.Checkout.TotalCOP != wantCOP {
		t.Errorf("TotalCOP = %d, se esperaba %d", result.Checkout.TotalCOP, wantCOP)
	}
	if result.Checkout.TotalUSD != wantUSD {
		t.Errorf("TotalUSD = %v, se esperaba %v", result.Checkout.TotalUSD, wantUSD)
	}
	if result.Checkout.ShippingCOP != 0 {
		t.Errorf("ShippingCOP = %d, la recogida en tienda no lleva envío", result.Checkout.ShippingCOP)
	}
	if len(result.Checkout.Items) != 2 {
		t.Fatalf("items = %d, se esperaban 2", len(result.Checkout.Items))
	}
	if result.Bold.AmountCOP != wantCOP {
		t.Errorf("monto cobrado = %d, se esperaba %d", result.Bold.AmountCOP, wantCOP)
	}
	if len(gateway.requests) != 1 {
		t.Fatalf("BuildCheckout llamado %d veces, se esperaba 1", len(gateway.requests))
	}
	if got := gateway.requests[0].RedirectionURL; got != testFront+"/carrito" {
		t.Errorf("RedirectionURL = %q, se esperaba %q", got, testFront+"/carrito")
	}
}

func TestCreateCheckout_SumaEnvioADomicilio(t *testing.T) {
	uc, _, _, _ := newCheckoutFixture()

	input := pickupInput()
	input.Fulfillment = sale.FulfillmentShipping
	input.Address = "Calle 123 #45-67"
	input.City = "Bogotá"
	input.Phone = "3001234567"

	result, err := uc.Execute(context.Background(), input, []int64{1, 2})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	wantCOP := int64(98000 + sale.ShippingCostCOP)
	if result.Checkout.TotalCOP != wantCOP {
		t.Errorf("TotalCOP = %d, se esperaba %d", result.Checkout.TotalCOP, wantCOP)
	}
	if result.Checkout.ShippingCOP != sale.ShippingCostCOP {
		t.Errorf("ShippingCOP = %d, se esperaba %d", result.Checkout.ShippingCOP, sale.ShippingCostCOP)
	}
	if result.Bold.AmountCOP != wantCOP {
		t.Errorf("monto cobrado = %d, se esperaba %d", result.Bold.AmountCOP, wantCOP)
	}
}

func TestCreateCheckout_RetieneElStock(t *testing.T) {
	uc, reservations, _, _ := newCheckoutFixture()

	if _, err := uc.Execute(context.Background(), pickupInput(), []int64{1, 2}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(reservations.extended) != 1 {
		t.Fatalf("ExtendExpiry llamado %d veces, se esperaba 1", len(reservations.extended))
	}
	call := reservations.extended[0]
	if call.minutes != 30 {
		t.Errorf("retención = %d min, se esperaban 30", call.minutes)
	}
}

func TestCreateCheckout_ReutilizaElCheckoutAbierto(t *testing.T) {
	uc, _, sales, _ := newCheckoutFixture()

	first, err := uc.Execute(context.Background(), pickupInput(), []int64{1, 2})
	if err != nil {
		t.Fatalf("primera llamada: %v", err)
	}

	// El cliente cerró el modal y volvió a pulsar Pagar: debe recibir la misma
	// referencia, no una nueva.
	second, err := uc.Execute(context.Background(), pickupInput(), []int64{2, 1})
	if err != nil {
		t.Fatalf("segunda llamada: %v", err)
	}
	if second.Checkout.Reference != first.Checkout.Reference {
		t.Errorf("referencia = %q, se esperaba la del checkout abierto %q",
			second.Checkout.Reference, first.Checkout.Reference)
	}
	if sales.createCalls != 1 {
		t.Errorf("CreateCheckout llamado %d veces, el checkout abierto debía reutilizarse", sales.createCalls)
	}
	if len(sales.sales) != 0 {
		t.Errorf("se registraron %d ventas; no debe haber ninguna", len(sales.sales))
	}
}

func TestCreateCheckout_CierraElCheckoutSiCambioElCarrito(t *testing.T) {
	uc, _, sales, _ := newCheckoutFixture()

	first, err := uc.Execute(context.Background(), pickupInput(), []int64{1, 2})
	if err != nil {
		t.Fatalf("primera llamada: %v", err)
	}

	// El cliente quitó una carta del carrito: el checkout anterior ya no sirve.
	if _, err := uc.Execute(context.Background(), pickupInput(), []int64{1}); err != nil {
		t.Fatalf("segunda llamada: %v", err)
	}

	if sales.checkouts[first.Checkout.Reference].Status != sale.CheckoutExpired {
		t.Errorf("el checkout anterior quedó %q, debía cerrarse como %q",
			sales.checkouts[first.Checkout.Reference].Status, sale.CheckoutExpired)
	}
	if sales.createCalls != 2 {
		t.Errorf("CreateCheckout llamado %d veces, se esperaban 2", sales.createCalls)
	}
}

func TestCreateCheckout_CierraElCheckoutVencido(t *testing.T) {
	uc, _, sales, _ := newCheckoutFixture()

	first, err := uc.Execute(context.Background(), pickupInput(), []int64{1, 2})
	if err != nil {
		t.Fatalf("primera llamada: %v", err)
	}
	// Se simula que pasó la ventana de retención.
	expired := sales.checkouts[first.Checkout.Reference]
	expired.ExpiresAt = time.Now().Add(-time.Minute)
	sales.checkouts[first.Checkout.Reference] = expired

	second, err := uc.Execute(context.Background(), pickupInput(), []int64{1, 2})
	if err != nil {
		t.Fatalf("segunda llamada: %v", err)
	}
	if second.Checkout.Reference == first.Checkout.Reference {
		t.Error("se reutilizó la referencia de un checkout vencido")
	}
	if sales.checkouts[first.Checkout.Reference].Status != sale.CheckoutExpired {
		t.Errorf("el checkout vencido quedó %q", sales.checkouts[first.Checkout.Reference].Status)
	}
}

func TestCreateCheckout_SinPasarelaConfigurada(t *testing.T) {
	uc, _, sales, _ := newCheckoutFixture()
	gateway := &fakeGateway{configured: false}
	uc = NewCreateCheckoutUseCase(uc.reservations, sales, gateway, 30, testFront)

	_, err := uc.Execute(context.Background(), pickupInput(), []int64{1, 2})
	if !errors.Is(err, payment.ErrNotConfigured) {
		t.Fatalf("err = %v, se esperaba ErrNotConfigured", err)
	}
	if sales.createCalls != 0 {
		t.Error("se creó un checkout sin pasarela configurada")
	}
}

func TestCreateCheckout_MontoMinimo(t *testing.T) {
	reservations := &fakeReservations{
		list: []reservation.Reservation{cartReservation(1, testUser, 500, 0.12, 1)},
	}
	sales := newFakeSales()
	uc := NewCreateCheckoutUseCase(reservations, sales, &fakeGateway{configured: true}, 30, testFront)

	_, err := uc.Execute(context.Background(), pickupInput(), []int64{1})
	if !errors.Is(err, payment.ErrAmountTooLow) {
		t.Fatalf("err = %v, se esperaba ErrAmountTooLow", err)
	}
}

func TestCreateCheckout_ExigeDatosDeEnvio(t *testing.T) {
	uc, _, _, _ := newCheckoutFixture()

	input := pickupInput()
	input.Fulfillment = sale.FulfillmentShipping
	input.City = "Bogotá"
	input.Phone = "3001234567"
	// Falta la dirección.

	_, err := uc.Execute(context.Background(), input, []int64{1, 2})
	if !errors.Is(err, sale.ErrInvalidInput) {
		t.Fatalf("err = %v, se esperaba ErrInvalidInput", err)
	}
}

func TestCreateCheckout_ReservaVencida(t *testing.T) {
	uc, _, sales, _ := newCheckoutFixture()

	// El cliente pide una reserva que ya no está en el carrito.
	_, err := uc.Execute(context.Background(), pickupInput(), []int64{1, 2, 99})
	if !errors.Is(err, sale.ErrReservationExpired) {
		t.Fatalf("err = %v, se esperaba ErrReservationExpired", err)
	}
	if sales.createCalls != 0 {
		t.Error("se creó un checkout con una reserva vencida")
	}
}

func TestCreateCheckout_CarritoVacio(t *testing.T) {
	uc, _, _, _ := newCheckoutFixture()

	if _, err := uc.Execute(context.Background(), pickupInput(), nil); !errors.Is(err, sale.ErrEmptyCart) {
		t.Fatalf("err = %v, se esperaba ErrEmptyCart", err)
	}
}

func TestCreateCheckout_ReferenciaValidaParaBold(t *testing.T) {
	uc, _, _, _ := newCheckoutFixture()

	result, err := uc.Execute(context.Background(), pickupInput(), []int64{1, 2})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	ref := result.Checkout.Reference
	if ref == "" {
		t.Fatal("referencia vacía")
	}
	if len(ref) > 60 {
		t.Errorf("referencia %q supera los 60 caracteres que admite Bold", ref)
	}
	// Bold solo acepta alfanuméricos, guiones y guiones bajos en el order-id.
	for _, r := range ref {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_'
		if !ok {
			t.Errorf("referencia %q contiene el carácter %q, que Bold no admite", ref, r)
		}
	}
}

// El checkout y sus reservas deben vencer juntos: si solo se moviera el stock,
// el barrido liberaría las cartas mientras el cliente sigue pagando.
func TestCreateCheckout_RenuevaLaExpiracionAlReintentar(t *testing.T) {
	uc, _, sales, _ := newCheckoutFixture()
	ctx := context.Background()

	first, err := uc.Execute(ctx, pickupInput(), []int64{1, 2})
	if err != nil {
		t.Fatalf("primera llamada: %v", err)
	}

	time.Sleep(20 * time.Millisecond)
	second, err := uc.Execute(ctx, pickupInput(), []int64{1, 2})
	if err != nil {
		t.Fatalf("segunda llamada: %v", err)
	}

	if !second.Checkout.ExpiresAt.After(first.Checkout.ExpiresAt) {
		t.Errorf("el checkout no se renovó: sigue venciendo en %v", second.Checkout.ExpiresAt)
	}
	if got := sales.checkouts[second.Checkout.Reference].ExpiresAt; !got.Equal(second.Checkout.ExpiresAt) {
		t.Errorf("lo persistido no coincide con lo devuelto: %v vs %v", got, second.Checkout.ExpiresAt)
	}
}

// Si el checkout se cerró entre la lectura y la renovación, se crea uno nuevo
// en vez de devolver un pago ya cerrado.
func TestCreateCheckout_RecreaElCheckoutSiSeCerraron(t *testing.T) {
	uc, _, sales, _ := newCheckoutFixture()
	ctx := context.Background()

	first, err := uc.Execute(ctx, pickupInput(), []int64{1, 2})
	if err != nil {
		t.Fatalf("primera llamada: %v", err)
	}
	// Simula que otro proceso lo liquidó o cerró.
	closed := sales.checkouts[first.Checkout.Reference]
	closed.Status = sale.CheckoutPaid
	sales.checkouts[first.Checkout.Reference] = closed

	second, err := uc.Execute(ctx, pickupInput(), []int64{1, 2})
	if err != nil {
		t.Fatalf("segunda llamada: %v", err)
	}
	if second.Checkout.Reference == first.Checkout.Reference {
		t.Fatal("se reutilizó la referencia de un checkout ya cerrado")
	}
	if second.Checkout.Status != sale.CheckoutOpen {
		t.Errorf("status = %q, se esperaba %q", second.Checkout.Status, sale.CheckoutOpen)
	}
}

func TestCreateCheckout_ReferenciasDistintasParaCarritosDistintos(t *testing.T) {
	uc, _, _, _ := newCheckoutFixture()

	first, err := uc.Execute(context.Background(), pickupInput(), []int64{1, 2})
	if err != nil {
		t.Fatalf("primera llamada: %v", err)
	}
	second, err := uc.Execute(context.Background(), pickupInput(), []int64{1})
	if err != nil {
		t.Fatalf("segunda llamada: %v", err)
	}
	if first.Checkout.Reference == second.Checkout.Reference {
		t.Error("dos carritos distintos compartieron la misma referencia de pago")
	}
}
