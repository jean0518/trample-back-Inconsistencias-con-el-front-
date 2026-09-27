package out

import (
	"context"

	"trample-back/internal/domain/catalog"
)

// ManualCardRepository da de alta cartas que el staff registra directamente en
// el inventario, sin pasar por Scrydex.
//
// Es un puerto aparte de CardRepository a propósito: la importación por API solo
// necesita SyncCard y GetVariantID, y colgarle el alta manual obligaría a todos
// esos casos de uso (y a sus dobles de prueba) a implementar un método que no
// usan.
type ManualCardRepository interface {
	// UpsertManualCard resuelve o crea expansión, carta y variante, y devuelve
	// los IDs persistidos junto con los datos canónicos que quedaron
	// guardados, que pueden diferir de los enviados: el nombre canónico de
	// Scrydex o el acabado normalizado. Es idempotente en cuanto al
	// external_id, así que reintentar el alta no duplica la carta.
	UpsertManualCard(ctx context.Context, card catalog.ManualCard) (catalog.ManualCardResult, error)
}
