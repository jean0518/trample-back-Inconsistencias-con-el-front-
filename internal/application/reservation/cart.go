package reservation

import (
	"context"
	"errors"

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
	if input.UserID <= 0 || input.CardID <= 0 || input.Quantity <= 0 {
		return reservation.Reservation{}, reservation.ErrInvalidListing
	}
	return uc.repo.Reserve(ctx, input, holdMinutes)
}

// Cart devuelve el contenido actual del carrito (reservas activas) del
// usuario.
func (uc *CartUseCase) Cart(ctx context.Context, userID int64) ([]reservation.Reservation, error) {
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
func (uc *CartUseCase) Remove(ctx context.Context, userID, id int64) error {
	return uc.repo.Remove(ctx, userID, id)
}

// AvailableStock devuelve el stock realmente comprable de una carta+idioma.
func (uc *CartUseCase) AvailableStock(ctx context.Context, cardID int64, language string) (int, error) {
	return uc.repo.AvailableStock(ctx, cardID, language)
}

// ListLogs devuelve el historial de reservas (panel admin), opcionalmente
// filtrado por estado (reserved | returned | sold).
func (uc *CartUseCase) ListLogs(ctx context.Context, status string, limit, offset int) ([]reservation.ReservationLog, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	logs, err := uc.repo.ListReservationLogs(ctx, status, limit, offset)
	if err != nil {
		return nil, err
	}
	if logs == nil {
		logs = []reservation.ReservationLog{}
	}
	return logs, nil
}

var ErrNotFound = reservation.ErrNotFound
var ErrInsufficientStock = reservation.ErrInsufficientStock

func IsInsufficientStock(err error) bool {
	return errors.Is(err, reservation.ErrInsufficientStock)
}
