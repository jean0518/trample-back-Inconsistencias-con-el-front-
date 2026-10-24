package postgres

import (
	"errors"
	"testing"

	"trample-back/internal/domain/sale"
)

func TestValidateOrderTransition_CreditoSoloSePuedeAnular(t *testing.T) {
	if err := validateOrderTransition(sale.StatusCredit, sale.StatusCredit); err != nil {
		t.Errorf("mantener crédito: %v", err)
	}
	if err := validateOrderTransition(sale.StatusCredit, sale.StatusCancelled); err != nil {
		t.Errorf("anular crédito: %v", err)
	}
	if err := validateOrderTransition(sale.StatusCredit, sale.StatusPaid); !errors.Is(err, sale.ErrOrderNotEditable) {
		t.Errorf("crédito -> pagado: err = %v", err)
	}
}
