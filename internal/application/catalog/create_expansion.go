package catalog

import (
	"context"

	"trample-back/internal/domain/catalog"
	"trample-back/internal/ports/out"
)

// NewExpansionUseCase crea una expansión desde el propio panel de inventario,
// sin que exista en Scrydex.
//
// Es la acción del botón "+" que acompaña al selector de expansión del
// formulario de alta manual. Devuelve la expansión creada para que el frontend
// la añada a su desplegable y deje al staff escribiendo el nombre de la carta
// sin recargar la página.
type NewExpansionUseCase struct {
	expansions out.ExpansionRepository
}

// NewNewExpansionUseCase construye el caso de uso.
func NewNewExpansionUseCase(expansions out.ExpansionRepository) *NewExpansionUseCase {
	return &NewExpansionUseCase{expansions: expansions}
}

// Execute valida la entrada, deriva el external_id y persiste. Es idempotente:
// si la expansión ya existe, la devuelve sin crearla otra vez, de modo que
// pulsar dos veces el botón no duplica el set ni rompe el formulario.
func (uc *NewExpansionUseCase) Execute(ctx context.Context, input catalog.NewExpansion) (catalog.NewExpansionResult, error) {
	validated, err := input.Validate()
	if err != nil {
		return catalog.NewExpansionResult{}, err
	}

	externalID, err := catalog.ExpansionExternalIDForManual(validated.Name)
	if err != nil {
		return catalog.NewExpansionResult{}, err
	}

	// El external_id va derivado del nombre para que el alta sea idempotente;
	// el repositorio resuelve el juego por código y devuelve la entidad con el
	// GameID ya resuelto.
	expansion, created, err := uc.expansions.CreateManual(ctx, validated.GameCode, catalog.Expansion{
		ExternalID:  externalID,
		Name:        validated.Name,
		Code:        validated.Code,
		Series:      validated.Series,
		Total:       validated.Total,
		ReleaseDate: validated.ReleaseDate,
	})
	if err != nil {
		return catalog.NewExpansionResult{}, err
	}

	return catalog.NewExpansionResult{Expansion: expansion, Created: created}, nil
}
