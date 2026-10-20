package reservation

import (
	"context"

	"trample-back/internal/domain/reservation"
	"trample-back/internal/ports/out"
)

const holdMinutes = 5

// CartUseCase agrupa las operaciones del carrito apoyadas en el stock
// temporal de 5 minutos.
type CartUseCase struct {
	repo out.ReservationRepository
}

func NewCartUseCase(repo out.ReservationRepository) *CartUseCase {
	return &CartUseCase{repo: repo}
}

// Add agrega N unidades de una carta en un idioma al carrito. Si ya hay una
// reserva activa del usuario para esa carta+idioma, la incrementa (renovando
// los 5 minutos). Valida contra el stock realmente comprable (el del
// inventario menos lo reservado por cualquier usuario), de modo que no se
// pueda exceder la disponibilidad aunque ya figure en el carrito.
func (uc *CartUseCase) Add(ctx context.Context, input reservation.ReserveInput) (reservation.Reservation, error) {
	if input.UserID == "" || input.CardID <= 0 || input.Quantity <= 0 {
		return reservation.Reservation{}, reservation.ErrInvalidListing
	}
	return uc.repo.Reserve(ctx, input, holdMinutes)
}

// Cart devuelve el contenido actual del carrito (reservas activas) del
// usuario.
func (uc *CartUseCase) Cart(ctx context.Context, userID string) ([]reservation.Reservation, error) {
	items, err := uc.repo.ListActiveByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []reservation.Reservation{}
	}
	return items, nil
}

// Remove libera una reserva activa del usuario y restaura el stock.
func (uc *CartUseCase) Remove(ctx context.Context, userID string, id int64) error {
	return uc.repo.Remove(ctx, userID, id)
}

// UpdateQuantity cambia la cantidad de una línea del carrito sin renovar los 5
// minutos, para que editar no sirva para alargar la reserva indefinidamente.
// Bajar libera stock; para quitar la línea está Remove.
func (uc *CartUseCase) UpdateQuantity(ctx context.Context, userID string, id int64, quantity int) (reservation.Reservation, error) {
	if quantity < 1 {
		return reservation.Reservation{}, reservation.ErrInvalidQuantity
	}
	return uc.repo.UpdateQuantity(ctx, userID, id, quantity)
}

// ListLogs devuelve el historial de reservas de cartas y productos (panel
// admin) con filtros, y el total sin paginar.
func (uc *CartUseCase) ListLogs(ctx context.Context, f reservation.LogFilter) ([]reservation.ReservationLog, int, error) {
	switch f.Status {
	case "", reservation.LogReserved, reservation.LogReturned, reservation.LogSold:
	default:
		return nil, 0, reservation.ErrInvalidLogFilter
	}
	switch f.Kind {
	case "", reservation.KindCard, reservation.KindProduct:
	default:
		return nil, 0, reservation.ErrInvalidLogFilter
	}
	if f.Limit <= 0 {
		f.Limit = 100
	}
	f.Limit = min(f.Limit, 500)
	f.Offset = max(f.Offset, 0)
	return uc.repo.ListReservationLogs(ctx, f)
}
