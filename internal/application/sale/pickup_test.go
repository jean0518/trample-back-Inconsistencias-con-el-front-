package sale

import (
	"context"
	"errors"
	"testing"
	"time"

	"trample-back/internal/domain/sale"
)

func storeInput() sale.ConfirmInput {
	return sale.ConfirmInput{
		UserID:        testUser,
		Fulfillment:   sale.FulfillmentPickup,
		PaymentMethod: sale.PaymentStore,
	}
}

// Un cliente (no staff) puede apartar un pedido de recogida: nace pendiente, con
// plazo de 2 días hábiles, y no se cuenta como pagado.
func TestConfirmSale_RecogidaEnTiendaNacePendiente(t *testing.T) {
	uc, _, sales := newConfirm()
	mailer := &fakeMailer{}
	uc.WithNotifier(NewPurchaseNotifier(fakeUsers{}, mailer))

	before := time.Now()
	result, err := uc.Execute(context.Background(), storeInput(), []int64{1, 2})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	s := result.Sale
	if s.Status != sale.StatusPendingPickup {
		t.Errorf("status = %q, se esperaba %q", s.Status, sale.StatusPendingPickup)
	}
	if s.PaymentMethod != sale.PaymentStore {
		t.Errorf("payment_method = %q", s.PaymentMethod)
	}
	if want := sale.PickupDeadlineFrom(before); s.PickupDeadline.Before(want.Add(-time.Second)) || s.PickupDeadline.After(sale.PickupDeadlineFrom(time.Now())) {
		t.Errorf("PickupDeadline = %v, se esperaba ~%v", s.PickupDeadline, want)
	}
	if len(sales.confirmedIDs) != 1 {
		t.Error("el pedido apartado debe confirmar las reservas (stock apartado)")
	}
	waitFor(t, &mailer.reserved, 1)
}

// "tienda" solo existe para recogida: con envío a domicilio no hay dónde pagar.
func TestConfirmSale_TiendaExigeRecogida(t *testing.T) {
	uc, _, sales := newConfirm()
	input := storeInput()
	input.Fulfillment = sale.FulfillmentShipping
	input.Address, input.City, input.Phone = "Calle 1 # 2-3", "Cali", "3001234567"

	if _, err := uc.Execute(context.Background(), input, []int64{1, 2}); !errors.Is(err, sale.ErrInvalidInput) {
		t.Fatalf("err = %v, se esperaba ErrInvalidInput", err)
	}
	if len(sales.sales) != 0 {
		t.Error("se registró un pedido con envío y pago en tienda")
	}
}

func pendingPickup(t *testing.T, deadline time.Time) (*PickupOrdersUseCase, *fakeSales, *fakeMailer, int64) {
	t.Helper()
	sales := newFakeSales()
	sales.nextSaleID = 6
	sales.sales[7] = sale.Sale{ID: 7, UserID: testUser, Status: sale.StatusPendingPickup, PickupDeadline: deadline}
	mailer := &fakeMailer{}
	return NewPickupOrdersUseCase(sales, NewPurchaseNotifier(fakeUsers{}, mailer)), sales, mailer, 7
}

func TestPickup_MarcarPagadoUnaSolaVez(t *testing.T) {
	uc, sales, mailer, id := pendingPickup(t, time.Now().Add(time.Hour))

	s, err := uc.MarkPaid(context.Background(), id)
	if err != nil {
		t.Fatalf("MarkPaid: %v", err)
	}
	if s.Status != sale.StatusPaid || sales.sales[id].Status != sale.StatusPaid {
		t.Errorf("el pedido no quedó pagado: %q", s.Status)
	}
	waitFor(t, &mailer.confirmed, 1)

	// Repetir no vuelve a pagar ni a mandar correo.
	if _, err := uc.MarkPaid(context.Background(), id); !errors.Is(err, sale.ErrNotPendingPickup) {
		t.Fatalf("segundo pago: err = %v, se esperaba ErrNotPendingPickup", err)
	}
	time.Sleep(50 * time.Millisecond)
	if got := mailer.confirmed.Load(); got != 1 {
		t.Errorf("correos de confirmación = %d, se esperaba 1", got)
	}
}

func TestPickup_CancelarDevuelveStockYAvisa(t *testing.T) {
	uc, sales, mailer, id := pendingPickup(t, time.Now().Add(time.Hour))

	if _, err := uc.Cancel(context.Background(), id); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if len(sales.restocked) != 1 {
		t.Errorf("stock devuelto %d veces, se esperaba 1", len(sales.restocked))
	}
	waitFor(t, &mailer.cancelled, 1)

	// Un pedido ya pagado no se puede cancelar por aquí.
	sales.sales[id] = sale.Sale{ID: id, UserID: testUser, Status: sale.StatusPaid}
	if _, err := uc.Cancel(context.Background(), id); !errors.Is(err, sale.ErrNotPendingPickup) {
		t.Fatalf("err = %v, se esperaba ErrNotPendingPickup", err)
	}
}

func TestPickup_VencidosSeCancelanYSoloEsos(t *testing.T) {
	uc, sales, mailer, id := pendingPickup(t, time.Now().Add(-time.Minute))
	sales.sales[8] = sale.Sale{ID: 8, UserID: testUser, Status: sale.StatusPendingPickup, PickupDeadline: time.Now().Add(time.Hour)}
	sales.sales[9] = sale.Sale{ID: 9, UserID: testUser, Status: sale.StatusPaid}

	n, err := uc.ExpireOverdue(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("ExpireOverdue = %d, %v; se esperaba 1, nil", n, err)
	}
	if sales.sales[id].Status != sale.StatusCancelled {
		t.Error("el pedido vencido no se canceló")
	}
	if sales.sales[8].Status != sale.StatusPendingPickup || sales.sales[9].Status != sale.StatusPaid {
		t.Error("se tocó un pedido que no estaba vencido")
	}
	waitFor(t, &mailer.cancelled, 1)
}
