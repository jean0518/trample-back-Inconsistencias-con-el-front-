package catalog

import (
	"fmt"
	"strings"
	"time"
)

// NewExpansion es una expansión que el staff crea desde el propio formulario de
// alta, sin que exista en Scrydex. Existe para poder registrar una carta sin
// tener que primero importar el set completo por API.
//
// El botón "+" del selector de expansión del formulario dispara este caso de
// uso y, si funciona, deja la expansión recién creada ya seleccionada, de modo
// que el staff pueda seguir escribiendo el nombre de la carta sin recargar la
// página ni perder lo que ya llenó.
type NewExpansion struct {
	GameCode string
	Name     string
	// Code, Series y ReleaseDate son opcionales: se rellenan si el staff los
	// tiene a mano y se guardan vacíos si no.
	Code        string
	Series      string
	ReleaseDate string
	Total       int
}

// NewExpansionResult lleva la expansión creada de vuelta para que el
// formulario la pueda añadir a su lista sin una segunda consulta.
type NewExpansionResult struct {
	Expansion Expansion
	Created   bool
}

// Validate normaliza los textos y comprueba lo mínimo indispensable. Devolver
// la copia normalizada evita que cada llamante tenga que acordarse de hacerlo.
func (n NewExpansion) Validate() (NewExpansion, error) {
	n.GameCode = strings.TrimSpace(n.GameCode)
	n.Name = strings.TrimSpace(n.Name)
	n.Code = strings.TrimSpace(n.Code)
	n.Series = strings.TrimSpace(n.Series)
	n.ReleaseDate = strings.TrimSpace(n.ReleaseDate)

	if n.GameCode == "" {
		return n, fmt.Errorf("el juego es requerido")
	}
	if n.Name == "" {
		return n, fmt.Errorf("el nombre de la expansión es requerido")
	}
	if n.Total < 0 {
		return n, fmt.Errorf("el total de cartas no puede ser negativo")
	}
	// Scrydex usa "YYYY-MM-DD"; se acepta también el formato con barras que
	// escriben a mano los usuarios y se normaliza, porque release_date se
	// guarda como texto y un formato distinto rompería los filtros por año.
	// Solo se aceptan formatos sin ambigüedad. Se descarta a propósito el
	// DD-MM-AAAA con guiones porque "06-09-2024" se leería como 9 de junio
	// mientras que "06/09/2024" sería 6 de septiembre: los mismos dígitos con
	// otro separador darían dos fechas distintas, que es peor que rechazarla.
	if n.ReleaseDate != "" {
		normalized, err := normalizeReleaseDate(n.ReleaseDate)
		if err != nil {
			return n, err
		}
		n.ReleaseDate = normalized
	}
	return n, nil
}


// ExpansionExternalIDForManual deriva el identificador de una expansión creada a
// mano. Se comparte con el alta de carta manual para que ambos caminos
// produzcan el mismo external_id y, por tanto, la misma fila.
func ExpansionExternalIDForManual(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("el nombre de la expansión es requerido")
	}
	return manualIDPrefix + slugOr(name, "expansion"), nil
}

// normalizeReleaseDate acepta "2024-09-06" (ISO, el que usa Scrydex) y
// "06/09/2024" (día/mes/año, como lo escribe el staff) y devuelve siempre
// "YYYY-MM-DD", que es lo que leen los filtros del catálogo.
func normalizeReleaseDate(raw string) (string, error) {
	for _, layout := range []string{"2006-01-02", "02/01/2006", time.RFC3339} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.Format("2006-01-02"), nil
		}
	}
	return "", fmt.Errorf("la fecha de lanzamiento no es válida (usa AAAA-MM-DD o DD/MM/AAAA)")
}
