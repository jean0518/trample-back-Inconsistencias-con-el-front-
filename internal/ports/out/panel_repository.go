package out

import "context"

// Panel representa una sección del panel administrativo.
type Panel struct {
	Code      string
	Name      string
	SortOrder int
}

// PanelRepository expone el catálogo de paneles disponibles en la BD.
type PanelRepository interface {
	GetAll(ctx context.Context) ([]Panel, error)
}