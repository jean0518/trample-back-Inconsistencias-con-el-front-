package sale

import (
	"context"
	"errors"
	"testing"
	"time"

	"trample-back/internal/domain/sale"
)

func pendingSaleForCredit(total int64) (*fakeSales, *sale.Sale) {
	sales := newFakeSales()
	s := sale.Sale{
		ID: 1, UserID: testUser, Status: sale.StatusPendingPickup, TotalCOP: total,
		Fulfillment: sale.FulfillmentPickup, PaymentMethod: sale.PaymentStore,
	}
	sales.sales[s.ID] = s
	return sales, &s
}

func TestBeforeToday_NoDesplazaFechaCivil(t *testing.T) {
	zone := time.FixedZone("COT", -5*60*60)
	now := time.Date(2026, 10, 22, 20, 0, 0, 0, zone)
	today := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	yesterday := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	if beforeToday(today, now) {
		t.Error("la fecha de hoy no debe contar como pasada")
	}
	if !beforeToday(yesterday, now) {
		t.Error("la fecha de ayer sí debe contar como pasada")
	}
}

func TestStoreCredit_CreaYRegistraAbonos(t *testing.T) {
	sales, s := pendingSaleForCredit(100_000)
	uc := NewStoreCreditsUseCase(sales)

	c, err := uc.Create(context.Background(), sale.NewCreditInput{
		SaleID: s.ID, InitialCOP: 30_000, DueDate: time.Now().Add(48 * time.Hour), Notes: "acuerdo",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if c.BalanceCOP != 70_000 || c.PaidCOP != 30_000 {
		t.Errorf("saldo/abonado = %d/%d, se esperaba 70000/30000", c.BalanceCOP, c.PaidCOP)
	}
	if sales.sales[s.ID].Status != sale.StatusCredit {
		t.Errorf("estado del pedido = %q, se esperaba %q", sales.sales[s.ID].Status, sale.StatusCredit)
	}

	c, err = uc.AddPayment(context.Background(), c.ID, 70_000, "saldo", testUser)
	if err != nil {
		t.Fatalf("AddPayment: %v", err)
	}
	if c.Status != sale.CreditPaid || c.BalanceCOP != 0 {
		t.Errorf("crédito = %q saldo %d, se esperaba paid/0", c.Status, c.BalanceCOP)
	}
	if sales.sales[s.ID].Status != sale.StatusPaid {
		t.Errorf("el pedido no quedó pagado: %q", sales.sales[s.ID].Status)
	}
}

func TestStoreCredit_ValidaAbonoInicialYFecha(t *testing.T) {
	sales, s := pendingSaleForCredit(100_000)
	uc := NewStoreCreditsUseCase(sales)

	if _, err := uc.Create(context.Background(), sale.NewCreditInput{
		SaleID: s.ID, InitialCOP: 100_001, DueDate: time.Now().Add(24 * time.Hour),
	}); !errors.Is(err, sale.ErrCreditPayment) {
		t.Errorf("abono mayor al total: err = %v, se esperaba ErrCreditPayment", err)
	}
	if _, err := uc.Create(context.Background(), sale.NewCreditInput{
		SaleID: s.ID, InitialCOP: 0, DueDate: time.Now().AddDate(0, 0, -2),
	}); !errors.Is(err, sale.ErrInvalidInput) {
		t.Errorf("fecha pasada: err = %v, se esperaba ErrInvalidInput", err)
	}

	sales.sales[s.ID] = sale.Sale{ID: s.ID, Status: sale.StatusPaid, TotalCOP: 100_000}
	if _, err := uc.Create(context.Background(), sale.NewCreditInput{
		SaleID: s.ID, DueDate: time.Now().Add(24 * time.Hour),
	}); !errors.Is(err, sale.ErrNotPendingPickup) {
		t.Errorf("pedido pagado: err = %v, se esperaba ErrNotPendingPickup", err)
	}
}

func TestStoreCredit_AbonoNoMayorAlSaldo(t *testing.T) {
	sales, s := pendingSaleForCredit(100_000)
	uc := NewStoreCreditsUseCase(sales)
	c, err := uc.Create(context.Background(), sale.NewCreditInput{
		SaleID: s.ID, InitialCOP: 0, DueDate: time.Now().Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := uc.AddPayment(context.Background(), c.ID, 100_001, "", testUser); !errors.Is(err, sale.ErrCreditPayment) {
		t.Errorf("err = %v, se esperaba ErrCreditPayment", err)
	}
}

func TestAdminOrders_PagarDesdeEdicionAvisaAlCliente(t *testing.T) {
	sales, _ := pendingSaleForCredit(100_000)
	mailer := &fakeMailer{}
	uc := NewAdminOrdersUseCase(sales, NewPurchaseNotifier(fakeUsers{}, mailer))

	s, err := uc.Update(context.Background(), sale.UpdateOrderInput{
		ID: 1, Address: "Calle 1", City: "Cali", Phone: "3001234567", Status: sale.StatusPaid,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if s.Status != sale.StatusPaid || !s.JustCreated {
		t.Errorf("pedido = %q justCreated=%v", s.Status, s.JustCreated)
	}
	waitFor(t, &mailer.confirmed, 1)
}

func TestAdminOrders_NoPermiteActivarCreditoEditando(t *testing.T) {
	sales, _ := pendingSaleForCredit(100_000)
	uc := NewAdminOrdersUseCase(sales, NewPurchaseNotifier(fakeUsers{}, &fakeMailer{}))

	if _, err := uc.Update(context.Background(), sale.UpdateOrderInput{ID: 1, Status: sale.StatusCredit}); !errors.Is(err, sale.ErrInvalidInput) {
		t.Errorf("err = %v, se esperaba ErrInvalidInput", err)
	}
}
