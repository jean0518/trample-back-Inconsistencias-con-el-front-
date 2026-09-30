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
	uc, reservations, _ := newConfirm()

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
	if len(reservations.confirmed) != 1 {
		t.Errorf("Confirm llamado %d veces, se esperaba 1", len(reservations.confirmed))
	}
}

// El pago con tarjeta NO se registra por esta vía: si lo hiciera, existiría un
// pedido antes de que el cliente pagara, que es justo lo que se quiere evitar.
func TestConfirmSale_RechazaPagoConTarjeta(t *testing.T) {
	uc, _, sales := newConfirm()

	if _, err := uc.Execute(context.Background(), pickupInput(), []int64{1, 2}); !errors.Is(err, sale.ErrInvalidInput) {
		t.Fatalf("err = %v, se esperaba ErrInvalidInput", err)
	}
	if len(sales.sales) != 0 {
		t.Fatalf("se registraron %d ventas para un pago en línea", len(sales.sales))
	}
}

func TestConfirmSale_ReservaVencidaNoCobraNada(t *testing.T) {
	uc, reservations, sales := newConfirm()

	input := sale.ConfirmInput{
		UserID:        testUser,
		Fulfillment:   sale.FulfillmentPickup,
		PaymentMethod: sale.PaymentTransfer,
	}
	// Se pide una reserva que ya no está activa.
	_, err := uc.Execute(context.Background(), input, []int64{1, 2, 99})
	if !errors.Is(err, sale.ErrReservationExpired) {
		t.Fatalf("err = %v, se esperaba ErrReservationExpired", err)
	}
	if len(reservations.confirmed) != 0 {
		t.Error("se confirmaron reservas de un pedido inválido")
	}
	if len(sales.sales) != 0 {
		t.Error("se registró una venta con una reserva vencida")
	}
}

func TestConfirmSale_CarreraAlConfirmar(t *testing.T) {
	uc, reservations, sales := newConfirm()
	// La reserva se vence entre listarla y confirmarla.
	reservations.confirmErr = reservation.ErrNotFound

	input := sale.ConfirmInput{
		UserID:        testUser,
		Fulfillment:   sale.FulfillmentPickup,
		PaymentMethod: sale.PaymentTransfer,
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
		// Falta ciudad y teléfono.
	}
	if _, err := uc.Execute(context.Background(), input, []int64{1, 2}); !errors.Is(err, sale.ErrInvalidInput) {
		t.Fatalf("err = %v, se esperaba ErrInvalidInput", err)
	}
	if len(sales.sales) != 0 {
		t.Error("se registró una venta sin datos de envío completos")
	}
}
