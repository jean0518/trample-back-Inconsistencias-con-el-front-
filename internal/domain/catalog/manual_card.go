package catalog

import (
	"fmt"
	"regexp"
	"strings"
)

// DefaultVariantName es el acabado que se guarda cuando el staff no indica uno.
// Usa el mismo valor que Scrydex devuelve para la carta estándar, de modo que
// una carta dada de alta a mano y esa misma carta importada desde la API
// terminen sobre una sola variante en lugar de duplicarse.
const DefaultVariantName = "non-foil"

// ManualIDPrefix evita que los identificadores derivados de un alta manual
// colisionen con los que asigna Scrydex (ej: "tcgp-001").
const ManualIDPrefix = "manual:"

// ManualCard es una carta que el staff del panel registra directamente en el
// inventario, sin consultarla en Scrydex. Reúne los datos de expansión, carta
// y acabado que se persisten en expansions / cards / card_variants, para que el
// alta manual reutilice el mismo modelo de datos que la importación desde la API.
type ManualCard struct {
	GameCode string
	// ExpansionID apunta a una expansión ya existente. Si es 0 se crea una
	// nueva con ExpansionName.
	ExpansionID   int64
	ExpansionName string
	// ExternalID permite reutilizar el identificador de una carta que ya se
	// importó desde Scrydex (para corregirla o completarla). Si está vacío se
	// deriva del nombre y el número.
	ExternalID  string
	Name        string
	Number      string
	Rarity      string
	VariantName string
}

// ManualCardResult son los identificadores que quedaron persistidos tras el
// alta. CardCreated y ExpansionCreated indican si la carta o la expansión no
// existían; si ya existían, el stock se sumó al listing de siempre.
type ManualCardResult struct {
	CardID           int64
	ExpansionID      int64
	VariantID        int64
	CardName         string
	ExternalID       string
	VariantName      string
	CardCreated      bool
	ExpansionCreated bool
}

// variantAliases equipara los acabados que el staff puede escribir a mano con
// los que devuelve Scrydex, para que ambos apunten a la misma variante.
var variantAliases = map[string]string{
	"":             DefaultVariantName,
	"normal":       DefaultVariantName,
	"non foil":     DefaultVariantName,
	"regular":      DefaultVariantName,
	"standard":     DefaultVariantName,
	"foil":         "foil",
	"holo":         "foil",
	"holographic":  "foil",
	"reverse holo": "reverse-holo",
	"reverse":      "reverse-holo",
	"holo reverse": "reverse-holo",
}

var nonSlugChars = regexp.MustCompile(`[^a-z0-9]+`)

// Validate comprueba los datos mínimos del alta y normaliza los textos libres
// (recorta espacios) para no crear cartas duplicadas por diferencias de tipeo
// en los bordes.
func (m ManualCard) Validate() (ManualCard, error) {
	m.GameCode = strings.TrimSpace(m.GameCode)
	m.ExpansionName = strings.TrimSpace(m.ExpansionName)
	m.ExternalID = strings.TrimSpace(m.ExternalID)
	m.Name = strings.TrimSpace(m.Name)
	m.Number = strings.TrimSpace(m.Number)
	m.Rarity = strings.TrimSpace(m.Rarity)
	m.VariantName = NormalizeVariantName(m.VariantName)

	if m.GameCode == "" {
		return m, fmt.Errorf("el juego es requerido")
	}
	if m.Name == "" {
		return m, fmt.Errorf("el nombre de la carta es requerido")
	}
	if m.ExpansionID <= 0 && m.ExpansionName == "" {
		return m, fmt.Errorf("selecciona una expansión o crea una nueva")
	}

	// El external_id se deja derivado aquí y no en el repositorio para que la
	// carta cruce la frontera del dominio ya canónica: quien la reciba no tiene
	// que saber de dónde se derivó ni volver a hacerlo.
	m.ExternalID = ManualExternalIDFor(m.ExternalID, m.Name, m.Number)
	return m, nil
}

// NormalizeVariantName traduce el acabado indicado al valor canónico que usa
// Scrydex. Lo desconocido se devuelve en minúsculas y sin espacios sobrantes,
// que es la forma en que Scrydex nombra sus variantes.
func NormalizeVariantName(name string) string {
	key := strings.ToLower(strings.TrimSpace(name))
	if canonical, ok := variantAliases[key]; ok {
		return canonical
	}
	return key
}

// ManualExternalIDFor devuelve el external_id a persistir: el que el staff
// envió si viene informado (así se puede corregir o completar una carta que ya
// venía de Scrydex) o, si no, uno derivado del nombre y el número.
//
// Derivar el identificador es lo que hace idempotente el alta: volver a
// registrar la misma carta suma stock a la existente en vez de crear una copia.
// El número entra en el identificador para distinguir versiones como "119" y
// "119a" de una misma carta.
func ManualExternalIDFor(explicit, name, number string) string {
	if explicit = strings.TrimSpace(explicit); explicit != "" {
		return explicit
	}
	base := slugOr(name, "carta")
	if num := slug(number); num != "" {
		base += "-" + num
	}
	return ManualIDPrefix + base
}


func slug(s string) string {
	return strings.Trim(nonSlugChars.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "-"), "-")
}

func slugOr(s, fallback string) string {
	if base := slug(s); base != "" {
		return base
	}
	return fallback
}
