package sale

import (
	"context"
	"errors"
	"testing"

	"trample-back/internal/domain/reservation"
	"trample-back/internal/domain/sale"
)

func newConfirm() (*ConfirmSaleUseCase, *fakeReservations, *fakeSales) {
	reservations := &fakeReservations{
		list: []reservation.Reservation{
			cartReservation(1, testUser, 45000, 11.25, 2),
			cartReservation(2, testUser, 8000, 2, 1),
		},
	}
	sales := newFakeSales()
	return NewConfirmSaleUseCase(reservations, sales), reservations, sales
}

func TestConfirmSale_EfectivoSoloAdmin(t *testing.T) {
	uc, _, sales := newConfirm()

	input := sale.ConfirmInput{
		UserID:        testUser,
		Fulfillment:   sale.FulfillmentPickup,
		PaymentMethod: sale.PaymentCash,
		IsAdmin:       false,
	}
	if _, err := uc.Execute(context.Background(), input, []int64{1, 2}); !errors.Is(err, sale.ErrInvalidInput) {
		t.Fatalf("err = %v, se esperaba ErrInvalidInput", err)
	}
	if len(sales.sales) != 0 {
		t.Error("se registró una venta para un cliente sin permisos")
	}
}

func TestConfirmSale_EfectivoDeAdmin(t *testing.T) {
	uc, _, sales := newConfirm()

	input := sale.ConfirmInput{
		UserID:        testUser,
		Fulfillment:   sale.FulfillmentPickup,
		PaymentMethod: sale.PaymentCash,
		IsAdmin:       true,
	}
	result, err := uc.Execute(context.Background(), input, []int64{1, 2})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Sale.Status != sale.StatusPaid {
		t.Errorf("status = %q, se esperaba %q", result.Sale.Status, sale.StatusPaid)
	}
	if result.Sale.TotalCOP != 98000 {
		t.Errorf("TotalCOP = %d, se esperaba 98000", result.Sale.TotalCOP)
	}
	if len(sales.confirmedIDs) != 1 || len(sales.confirmedIDs[0]) != 2 {
		t.Errorf("la venta debía confirmar las 2 reservas, confirmó %v", sales.confirmedIDs)
	}
}

// El pago con tarjeta NO se registra por esta vía: si lo hiciera, existiría un
// pedido antes de que el cliente pagara, que es justo lo que se quiere evitar.
func TestConfirmSale_RechazaPagoConTarjeta(t *testing.T) {
	uc, _, sales := newConfirm()

	if _, err := uc.Execute(context.Background(), onlineInput(), []int64{1, 2}); !errors.Is(err, sale.ErrInvalidInput) {
		t.Fatalf("err = %v, se esperaba ErrInvalidInput", err)
	}
	if len(sales.sales) != 0 {
		t.Fatalf("se registraron %d ventas para un pago en línea", len(sales.sales))
	}
}

func TestConfirmSale_ReservaVencidaNoCobraNada(t *testing.T) {
	uc, _, sales := newConfirm()

	input := sale.ConfirmInput{
		UserID:        testUser,
		Fulfillment:   sale.FulfillmentPickup,
		PaymentMethod: sale.PaymentTransfer,
		IsAdmin:       true,
	}
	// Se pide una reserva que ya no está activa.
	_, err := uc.Execute(context.Background(), input, []int64{1, 2, 99})
	if !errors.Is(err, sale.ErrReservationExpired) {
		t.Fatalf("err = %v, se esperaba ErrReservationExpired", err)
	}
	if len(sales.confirmedIDs) != 0 {
		t.Error("se confirmaron reservas de un pedido inválido")
	}
	if len(sales.sales) != 0 {
		t.Error("se registró una venta con una reserva vencida")
	}
}

func TestConfirmSale_CarreraAlConfirmar(t *testing.T) {
	uc, _, sales := newConfirm()
	// La reserva se vence entre listarla y confirmarla (dentro de Create).
	sales.confirmErr = reservation.ErrNotFound

	input := sale.ConfirmInput{
		UserID:        testUser,
		Fulfillment:   sale.FulfillmentPickup,
		PaymentMethod: sale.PaymentTransfer,
		IsAdmin:       true,
	}
	_, err := uc.Execute(context.Background(), input, []int64{1, 2})
	if !errors.Is(err, sale.ErrReservationExpired) {
		t.Fatalf("err = %v, se esperaba ErrReservationExpired", err)
	}
	if len(sales.sales) != 0 {
		t.Error("se registró una venta aunque la reserva se perdió en la carrera")
	}
}

func TestConfirmSale_ExigeDatosDeEnvio(t *testing.T) {
	uc, _, sales := newConfirm()

	input := sale.ConfirmInput{
		UserID:        testUser,
		Fulfillment:   sale.FulfillmentShipping,
		Address:       "Calle 123",
		PaymentMethod: sale.PaymentTransfer,
		IsAdmin:       true,
		// Falta ciudad y teléfono.
	}
	if _, err := uc.Execute(context.Background(), input, []int64{1, 2}); !errors.Is(err, sale.ErrInvalidInput) {
		t.Fatalf("err = %v, se esperaba ErrInvalidInput", err)
	}
	if len(sales.sales) != 0 {
		t.Error("se registró una venta sin datos de envío completos")
	}
}

// Una transferencia registra una venta ya pagada: si un cliente pudiera elegirla
// obtendría el pedido y el inventario descontado sin pagar.
func TestConfirmSale_TransferenciaSoloStaff(t *testing.T) {
	uc, _, sales := newConfirm()

	input := sale.ConfirmInput{
		UserID:        testUser,
		Fulfillment:   sale.FulfillmentPickup,
		PaymentMethod: sale.PaymentTransfer,
		IsAdmin:       false,
	}
	if _, err := uc.Execute(context.Background(), input, []int64{1, 2}); !errors.Is(err, sale.ErrInvalidInput) {
		t.Fatalf("err = %v, se esperaba ErrInvalidInput", err)
	}
	if len(sales.sales) != 0 {
		t.Error("un cliente obtuvo una venta pagada por transferencia sin pagar")
	}
	if len(sales.confirmedIDs) != 0 {
		t.Error("se confirmaron las reservas de un cliente sin permisos")
	}
}

// Si el inventario cambió mientras la carta estaba reservada, la venta falla sin
// confirmar nada (el repositorio hace ambas cosas en una transacción).
func TestConfirmSale_SinStockNoRegistraLaVenta(t *testing.T) {
	uc, _, sales := newConfirm()
	sales.confirmErr = sale.ErrInsufficientStock

	input := sale.ConfirmInput{
		UserID:        testUser,
		Fulfillment:   sale.FulfillmentPickup,
		PaymentMethod: sale.PaymentTransfer,
		IsAdmin:       true,
	}
	if _, err := uc.Execute(context.Background(), input, []int64{1, 2}); !errors.Is(err, sale.ErrInsufficientStock) {
		t.Fatalf("err = %v, se esperaba ErrInsufficientStock", err)
	}
	if len(sales.sales) != 0 {
		t.Error("se registró una venta sin stock")
	}
}

// Los espacios no cuentan como dato: "   " no es una dirección.
func TestConfirmSale_DireccionSoloEspaciosEsInvalida(t *testing.T) {
	uc, _, sales := newConfirm()

	input := sale.ConfirmInput{
		UserID:        testUser,
		Fulfillment:   sale.FulfillmentShipping,
		Address:       "   ",
		City:          "Bogotá",
		Phone:         "3001234567",
		PaymentMethod: sale.PaymentTransfer,
		IsAdmin:       true,
	}
	if _, err := uc.Execute(context.Background(), input, []int64{1, 2}); !errors.Is(err, sale.ErrInvalidInput) {
		t.Fatalf("err = %v, se esperaba ErrInvalidInput", err)
	}
	if len(sales.sales) != 0 {
		t.Error("se registró un envío sin dirección")
	}
}

func TestValidateFulfillment_Telefono(t *testing.T) {
	base := sale.ConfirmInput{Fulfillment: sale.FulfillmentShipping, Address: "Calle 1", City: "Cali"}
	tests := map[string]bool{
		"3001234567":       true,
		"+57 300 123 4567": true,
		"123":              false,
		"abc":              false,
		"1234567890123456": false,
	}
	for phone, ok := range tests {
		in := base
		in.Phone = phone
		err := validateFulfillment(normalizeFulfillment(in))
		if ok && err != nil {
			t.Errorf("teléfono %q rechazado: %v", phone, err)
		}
		if !ok && err == nil {
			t.Errorf("teléfono %q aceptado, debía rechazarse", phone)
		}
	}
}

// Un precio con decimales se redondea, no se trunca.
func TestPriceCart_RedondeaElPrecio(t *testing.T) {
	reservations := &fakeReservations{list: []reservation.Reservation{
		cartReservation(1, testUser, 12345.9, 3, 2),
	}}
	cart, err := priceCart(context.Background(), reservations, nil, testUser, []int64{1}, nil, sale.FulfillmentPickup)
	if err != nil {
		t.Fatalf("priceCart: %v", err)
	}
	if cart.totalCOP != 12346*2 {
		t.Errorf("totalCOP = %d, se esperaba %d", cart.totalCOP, 12346*2)
	}
	if cart.items[0].PriceCOP != 12346 {
		t.Errorf("precio del item = %d, se esperaba 12346", cart.items[0].PriceCOP)
	}
}
