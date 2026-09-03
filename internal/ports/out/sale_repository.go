package out

import (
	"context"
	"time"

	"trample-back/internal/domain/sale"
)

// SaleRepository persiste ventas locales y sus items. No se conecta con
// ninguna pasarela de pagos externa todavía.
type SaleRepository interface {
	// Create persiste la venta y sus items en una transacción atómica y
	// devuelve la venta creada con su ID.
	Create(ctx context.Context, s sale.Sale) (sale.Sale, error)
	ListByUser(ctx context.Context, userID int64, limit, offset int) ([]sale.Sale, error)
	ListAll(ctx context.Context, limit, offset int) ([]sale.Sale, error)
	// Stats agrega las ventas en buckets desde `since` hasta ahora. period
	// admite "day" (agrupa por día) o "week" (agrupa por semana ISO).
	Stats(ctx context.Context, period string, since time.Time) ([]sale.StatBucket, error)
}
