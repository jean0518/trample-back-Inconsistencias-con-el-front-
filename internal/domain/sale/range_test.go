package sale

import (
	"errors"
	"testing"
	"time"
)

func TestParseDateRange(t *testing.T) {
	from, to, err := ParseDateRange("2026-10-01", "2026-10-31")
	if err != nil {
		t.Fatal(err)
	}
	// Medianoche en Colombia (05:00 UTC) y el día `to` completo.
	if !from.Equal(time.Date(2026, 10, 1, 5, 0, 0, 0, time.UTC)) || !to.Equal(time.Date(2026, 11, 1, 5, 0, 0, 0, time.UTC)) {
		t.Errorf("rango = %v .. %v", from, to)
	}
	if f, tt, err := ParseDateRange("", ""); err != nil || !f.IsZero() || !tt.IsZero() {
		t.Errorf("sin fechas: %v %v %v", f, tt, err)
	}
	if _, _, err := ParseDateRange("2026-10-05", "2026-10-04"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("invertido: err = %v", err)
	}
	if _, _, err := ParseDateRange("05/10/2026", ""); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("formato: err = %v", err)
	}
}
