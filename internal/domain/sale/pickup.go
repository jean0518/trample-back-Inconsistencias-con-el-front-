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
