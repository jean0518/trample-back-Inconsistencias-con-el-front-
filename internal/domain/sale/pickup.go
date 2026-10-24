package sale

import "time"

// storeZone es la hora de Colombia (UTC-5, sin horario de verano). Es una zona
// fija a propósito: no depende de que el contenedor traiga la base de zonas.
var storeZone = time.FixedZone("COT", -5*60*60)

// PickupDeadlineFrom devuelve el límite para recoger un pedido hecho en `from`:
// el final (23:59:59, hora de Colombia) del segundo día hábil siguiente.
// Cuenta lunes a viernes; no conoce los festivos.
//
// Ej.: un pedido del viernes vence el martes; uno del sábado, también el martes.
func PickupDeadlineFrom(from time.Time) time.Time {
	d := from.In(storeZone)
	day := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, storeZone)
	for n := 0; n < PickupBusinessDays; {
		day = day.AddDate(0, 0, 1)
		if wd := day.Weekday(); wd != time.Saturday && wd != time.Sunday {
			n++
		}
	}
	return day.Add(24*time.Hour - time.Second)
}

// ExtendPickupDeadline corre la fecha límite de una recogida `extraBusinessDays`
// días hábiles. Si el plazo ya venció (o no existe), cuenta desde `now`: así una
// extensión siempre deja al cliente un plazo vigente. Devuelve el final del día
// (23:59:59, hora de Colombia), igual que PickupDeadlineFrom.
func ExtendPickupDeadline(current, now time.Time, extraBusinessDays int) time.Time {
	if extraBusinessDays < 1 {
		return current
	}
	base := current
	if base.IsZero() || base.Before(now) {
		base = now
	}
	d := base.In(storeZone)
	day := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, storeZone)
	for n := 0; n < extraBusinessDays; {
		day = day.AddDate(0, 0, 1)
		if wd := day.Weekday(); wd != time.Saturday && wd != time.Sunday {
			n++
		}
	}
	return day.Add(24*time.Hour - time.Second)
}

// ParseDateRange convierte las fechas YYYY-MM-DD de un filtro (hora de
// Colombia) en el intervalo [from, to) que cubre ambos días completos. Las
// fechas vacías no limitan (devuelven cero). Error si alguna no es válida o si
// `to` es anterior a `from`.
func ParseDateRange(from, to string) (time.Time, time.Time, error) {
	var start, end time.Time
	if from != "" {
		d, err := time.ParseInLocation("2006-01-02", from, storeZone)
		if err != nil {
			return time.Time{}, time.Time{}, ErrInvalidInput
		}
		start = d
	}
	if to != "" {
		d, err := time.ParseInLocation("2006-01-02", to, storeZone)
		if err != nil {
			return time.Time{}, time.Time{}, ErrInvalidInput
		}
		end = d.AddDate(0, 0, 1) // el día `to` cuenta completo
	}
	if !start.IsZero() && !end.IsZero() && !end.After(start) {
		return time.Time{}, time.Time{}, ErrInvalidInput
	}
	return start, end, nil
}
