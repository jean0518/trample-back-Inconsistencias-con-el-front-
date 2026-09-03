package sale

import (
	"context"
	"time"

	"trample-back/internal/domain/sale"
	"trample-back/internal/ports/out"
)

// SaleStatsUseCase devuelve un dashboard de ventas agregadas por día o por
// semana. period admite "day" (últimos `buckets` días) o "week" (últimas
// `buckets` semanas).
type SaleStatsUseCase struct {
	repo out.SaleRepository
}

func NewSaleStatsUseCase(repo out.SaleRepository) *SaleStatsUseCase {
	return &SaleStatsUseCase{repo: repo}
}

func (uc *SaleStatsUseCase) Stats(ctx context.Context, period string, buckets int) ([]sale.StatBucket, error) {
	if buckets <= 0 {
		buckets = 14
	}
	if buckets > 90 {
		buckets = 90
	}
	if period != "week" {
		period = "day"
	}

	now := time.Now()
	daysBack := buckets
	if period == "week" {
		daysBack = buckets * 7
	}
	since := now.AddDate(0, 0, -daysBack)

	return uc.repo.Stats(ctx, period, since)
}
