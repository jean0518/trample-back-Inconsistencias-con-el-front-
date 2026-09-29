package out

import (
	"context"

	"trample-back/internal/domain/catalog"
)

// PriceUpdateNotifier avisa al staff del resultado de un evento de precios de
// Scrydex: qué precios del inventario cambiaron y qué expansiones no se
// procesaron. Es un canal informativo: si falla, los precios ya quedaron
// actualizados y el error solo se registra.
type PriceUpdateNotifier interface {
	NotifyPriceReport(ctx context.Context, report catalog.PriceReport) error
}
