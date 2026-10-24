package catalog

import "errors"

var (
	ErrGameNotFound = errors.New("juego no encontrado")
	ErrGameHasCards = errors.New("no se puede eliminar el juego porque tiene cartas o expansiones asociadas")
)

type Game struct {
	ID          int64
	Code        string
	Name        string
	ShortName   string
	Accent      string
	Tagline     string
	IconURL     string
	LogoURL     string
	IconImageID *int64
	LogoImageID *int64
	HasCards    bool
	Active      bool
	// Solo se rellenan en el listado administrativo.
	ProductsCount     int64
	CardsCount        int64 // cartas con stock activo en inventario
	CatalogCardsCount int64 // cartas registradas en el catálogo
	ExpansionsCount   int64
}

type GameInput struct {
	Code      string
	Name      string
	ShortName string
	Accent    string
	Tagline   string
	IconURL   string
	LogoURL   string
	HasCards  *bool
	Active    *bool
}
