package out

import (
	"context"
	"trample-back/internal/domain/catalog"
)

type ExpansionRepository interface {
	SyncExpansions(ctx context.Context, gameCode string, expansions []catalog.Expansion) error
	ListExpansions(ctx context.Context, gameCode string) ([]catalog.Expansion, error)
	ListByGameID(ctx context.Context, gameID int64) ([]catalog.Expansion, error)
	ListAll(ctx context.Context) ([]catalog.Expansion, error)
	// CreateManual registra una expansión que no viene de Scrydex, con el
	// external_id derivado del nombre. Si ya existe una con ese identificador
	// la devuelve tal cual (Created=false) en vez de duplicarla, que es lo que
	// permite que el botón "+" del formulario sea idempotente.
	CreateManual(ctx context.Context, gameCode string, e catalog.Expansion) (catalog.Expansion, bool, error)
}
