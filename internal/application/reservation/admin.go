package reservation

import (
	"context"

	"trample-back/internal/domain/reservation"
)

// cardReleaser libera una reserva de carta activa por id, sin filtrar por
// usuario. Lo implementa el repositorio de reservas de cartas.
type cardReleaser interface {
	ReleaseActiveByID(ctx context.Context, id int64) error
}

// productReleaser libera una reserva de producto activa por id.
type productReleaser interface {
	ReleaseActiveByID(ctx context.Context, id int64) error
}

// AdminUseCase agrupa las acciones del panel sobre reservas de ambos módulos.
type AdminUseCase struct {
	cards    cardReleaser
	products productReleaser
}

func NewAdminUseCase(cards cardReleaser, products productReleaser) *AdminUseCase {
	return &AdminUseCase{cards: cards, products: products}
}

// Release libera una reserva activa. kind debe ser "card" o "product".
func (uc *AdminUseCase) Release(ctx context.Context, kind string, id int64) error {
	if id <= 0 {
		return reservation.ErrNotFound
	}
	switch kind {
	case reservation.KindCard:
		return uc.cards.ReleaseActiveByID(ctx, id)
	case reservation.KindProduct:
		return uc.products.ReleaseActiveByID(ctx, id)
	default:
		return reservation.ErrInvalidKind
	}
}
