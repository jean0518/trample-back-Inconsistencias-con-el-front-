package sale

import (
	"testing"
	"time"
)

func TestExtendPickupDeadline(t *testing.T) {
	cot := time.FixedZone("COT", -5*3600)
	cases := []struct {
		name    string
		current time.Time
		now     time.Time
		days    int
		want    string
	}{
		{
			"suma desde el plazo vigente",
			time.Date(2026, 10, 5, 23, 59, 59, 0, cot),
			time.Date(2026, 10, 5, 10, 0, 0, 0, cot),
			1,
			"2026-10-06 23:59:59",
		},
		{
			"salta el fin de semana",
			time.Date(2026, 10, 9, 23, 59, 59, 0, cot),
			time.Date(2026, 10, 9, 10, 0, 0, 0, cot),
			1,
			"2026-10-12 23:59:59",
		},
		{
			"si venció cuenta desde hoy",
			time.Date(2026, 10, 1, 23, 59, 59, 0, cot),
			time.Date(2026, 10, 8, 10, 0, 0, 0, cot),
			2,
			"2026-10-12 23:59:59",
		},
		{
			"sin plazo previo cuenta desde hoy",
			time.Time{},
			time.Date(2026, 10, 10, 12, 0, 0, 0, cot),
			2,
			"2026-10-13 23:59:59",
		},
		{
			"cero días no cambia nada",
			time.Date(2026, 10, 5, 23, 59, 59, 0, cot),
			time.Date(2026, 10, 5, 10, 0, 0, 0, cot),
			0,
			"2026-10-05 23:59:59",
		},
	}
	for _, c := range cases {
		got := ExtendPickupDeadline(c.current, c.now, c.days).In(cot).Format("2006-01-02 15:04:05")
		if got != c.want {
			t.Errorf("%s: %s, se esperaba %s", c.name, got, c.want)
		}
	}
}

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
