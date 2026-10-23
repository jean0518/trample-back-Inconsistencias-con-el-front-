// Package product modela los productos propios de la tienda que no son cartas
// sueltas (cajas de sobres, sobres, mazos, fundas, etc.).
//
// Es un módulo deliberadamente separado del catálogo de cartas: no hay
// expansiones, variantes, idiomas ni propietarios. Un producto es un título,
// una descripción opcional, un precio en COP, un stock y hasta cinco fotos.
package product

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// MaxImages es el máximo de fotos por producto.
	MaxImages = 5

	// MaxTitleLength y MaxDescriptionLength acotan los textos libres para que
	// un valor a mano no engorde la base ni rompa las tarjetas del catálogo.
	MaxTitleLength       = 200
	MaxDescriptionLength = 5000

	// MaxPriceCOP evita un cero de más al digitar el precio. Es un tope de
	// cordura, no una regla de negocio.
	MaxPriceCOP int64 = 100_000_000

	// MaxStock es el techo de unidades por producto, por la misma razón.
	MaxStock = 100_000

	StatusActive = "active"
	StatusHidden = "hidden"

	// Juegos admitidos para clasificar productos sellados. El front usa estos
	// mismos códigos (GameId) para filtrar y editar en bloque.
	GameMagic     = "magic"
	GamePokemon   = "pokemon"
	GameRiftbound = "riftbound"
)

var (
	ErrNotFound          = errors.New("producto no encontrado")
	ErrImageNotFound     = errors.New("imagen del producto no encontrada")
	ErrTooManyImages     = fmt.Errorf("un producto admite máximo %d fotos", MaxImages)
	ErrHasSales          = errors.New("el producto tiene ventas: ocúltalo en vez de borrarlo")
	ErrInsufficientStock = errors.New("stock insuficiente para reservar")
	ErrHidden            = errors.New("el producto no está disponible")
	// ErrImageAttachment indica que el producto se guardó pero alguna foto no.
	// Es un caso aparte de propósito: el alta no es idempotente (dos productos
	// pueden llamarse igual), así que reenviar el formulario lo duplicaría y el
	// handler responde 422 devolviendo el producto ya creado.
	ErrImageAttachment = errors.New("el producto se guardó pero una foto falló")
)

// Image es una foto del producto. Position 1 es la principal.
type Image struct {
	ID       int64  `json:"id"`
	Position int    `json:"position"`
	URL      string `json:"url"`
}

// Product es un producto de la tienda con su disponibilidad calculada.
type Product struct {
	ID          int64
	Title       string
	Description string
	PriceCOP    int64
	// Stock son las unidades totales del producto; Reserved las que hoy sostienen
	// carritos o pagos en curso. Available es lo realmente comprable.
	Stock    int
	Reserved int
	Status   string
	Language string
	// Game clasifica el producto por juego (magic, pokemon, riftbound). Vacío
	// significa sin especificar.
	Game      string
	Images    []Image
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Available devuelve las unidades que un cliente puede reservar ahora mismo.
func (p Product) Available() int {
	if n := p.Stock - p.Reserved; n > 0 {
		return n
	}
	return 0
}

// SoldOut indica que no queda nada comprable. Se deriva y no se guarda: el
// estado "Agotado" cambia solo con cada reserva, venta o reposición.
func (p Product) SoldOut() bool {
	return p.Available() == 0
}

// Input son los datos editables de un producto.
type Input struct {
	Title       string
	Description string
	PriceCOP    int64
	Stock       int
	Language    string
	Game        string
}

// Validate normaliza (recorta espacios) y valida el producto.
func (in Input) Validate() (Input, error) {
	in.Title = strings.TrimSpace(in.Title)
	in.Description = strings.TrimSpace(in.Description)
	in.Game = strings.TrimSpace(in.Game)
	in.Language = strings.TrimSpace(in.Language)

	if !ValidGame(in.Game) {
		return in, fmt.Errorf("el juego debe ser %q, %q, %q o vacío", GameMagic, GamePokemon, GameRiftbound)
	}
	if in.Title == "" {
		return in, errors.New("el título es requerido")
	}
	if utf8.RuneCountInString(in.Title) > MaxTitleLength {
		return in, fmt.Errorf("el título supera los %d caracteres", MaxTitleLength)
	}
	if utf8.RuneCountInString(in.Description) > MaxDescriptionLength {
		return in, fmt.Errorf("la descripción supera los %d caracteres", MaxDescriptionLength)
	}
	if in.PriceCOP <= 0 {
		return in, errors.New("el precio debe ser mayor a 0")
	}
	if in.PriceCOP > MaxPriceCOP {
		return in, fmt.Errorf("el precio supera el máximo de %d COP", MaxPriceCOP)
	}
	if in.Stock < 0 {
		return in, errors.New("el stock no puede ser negativo")
	}
	if in.Stock > MaxStock {
		return in, fmt.Errorf("el stock supera el máximo de %d unidades", MaxStock)
	}
	return in, nil
}

// ValidStatus indica si el estado es uno de los dos admitidos.
func ValidStatus(status string) bool {
	return status == StatusActive || status == StatusHidden
}

// ValidGame indica si el código de juego es uno de los admitidos. Vacío es
// válido y significa "sin especificar".
func ValidGame(game string) bool {
	switch game {
	case "", GameMagic, GamePokemon, GameRiftbound:
		return true
	default:
		return false
	}
}

// ListFilter acota el listado. IncludeHidden es solo para el panel admin.
type ListFilter struct {
	Query         string
	IncludeHidden bool
	Game          string
	Status        string
	Language      string
	// SoldOut filtra por disponibilidad. nil = sin filtro; true = agotados;
	// false = con stock.
	SoldOut *bool
	// Sort ordena el listado: relevance (por defecto), price-asc, price-desc,
	// name o newest.
	Sort   string
	Limit  int
	Offset int
}

const (
	DefaultListLimit = 24
	MaxListLimit     = 100
)

// Normalize aplica los límites de paginación.
func (f ListFilter) Normalize() ListFilter {
	f.Query = strings.TrimSpace(f.Query)
	f.Sort = strings.TrimSpace(f.Sort)
	if f.Limit <= 0 {
		f.Limit = DefaultListLimit
	}
	if f.Limit > MaxListLimit {
		f.Limit = MaxListLimit
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	return f
}

// ValidateImageURL comprueba que la URL de una foto tenga una forma que el
// navegador pueda cargar. Se valida antes de tocar la base para no dejar un
// producto creado con una foto inalcanzable.
func ValidateImageURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return errors.New("la URL de la imagen está vacía")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("la URL de la imagen no es válida: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return errors.New("la URL de la imagen debe empezar por http:// o https://")
	}
	return nil
}
