package out

import (
	"context"

	"trample-back/internal/domain/catalog"
)

// PriceUpdateNotifier avisa al staff de los precios que cambiaron en el
// inventario tras un evento de Scrydex. Es un canal informativo: si falla, el
// precio ya quedó actualizado y el error solo se registra.
type PriceUpdateNotifier interface {
	NotifyPriceUpdates(ctx context.Context, gameCode string, changes []catalog.CardPriceChange) error
}
