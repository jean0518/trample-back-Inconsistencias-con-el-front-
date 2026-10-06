package sale

import (
	"testing"
	"time"
)

func TestPickupDeadlineFrom(t *testing.T) {
	cot := time.FixedZone("COT", -5*3600)
	cases := []struct {
		name string
		from time.Time
		want string
	}{
		{"lunes", time.Date(2026, 10, 5, 10, 0, 0, 0, cot), "2026-10-07 23:59:59"},
		{"jueves", time.Date(2026, 10, 8, 9, 0, 0, 0, cot), "2026-10-12 23:59:59"},
		{"viernes", time.Date(2026, 10, 9, 17, 0, 0, 0, cot), "2026-10-13 23:59:59"},
		{"sábado", time.Date(2026, 10, 10, 12, 0, 0, 0, cot), "2026-10-13 23:59:59"},
		{"domingo", time.Date(2026, 10, 11, 12, 0, 0, 0, cot), "2026-10-13 23:59:59"},
		// 23:30 en Colombia ya es el día siguiente en UTC: cuenta el día local.
		{"lunes tarde (UTC martes)", time.Date(2026, 10, 6, 4, 30, 0, 0, time.UTC), "2026-10-07 23:59:59"},
	}
	for _, c := range cases {
		got := PickupDeadlineFrom(c.from).In(cot).Format("2006-01-02 15:04:05")
		if got != c.want {
			t.Errorf("%s: %s, se esperaba %s", c.name, got, c.want)
		}
	}
}
