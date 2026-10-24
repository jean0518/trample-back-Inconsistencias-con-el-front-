package sale

import (
	"testing"
	"time"
)

func TestStoreCredit_OverdueComparaFechaCivil(t *testing.T) {
	now := time.Date(2026, 10, 22, 20, 0, 0, 0, storeZone)
	credit := StoreCredit{Status: CreditOpen, BalanceCOP: 1}

	credit.DueDate = time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	if credit.Overdue(now) {
		t.Error("un crédito que vence hoy no está vencido")
	}

	credit.DueDate = time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	if !credit.Overdue(now) {
		t.Error("un crédito que venció ayer sí está vencido")
	}

	credit.DueDate = time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	credit.Status = CreditPaid
	if credit.Overdue(now) {
		t.Error("un crédito pagado no está vencido")
	}
}
