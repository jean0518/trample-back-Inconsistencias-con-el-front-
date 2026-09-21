package out

import (
	"context"
	"trample-back/internal/domain/reservation"
)

// ReservationRepository gestiona el stock temporal (5 min) que se descuenta
// del inventario al agregar una carta al carrito y se restaura si el cliente
// abandona o expira.
type ReservationRepository interface {
	// Reserve crea (o incrementa) la reserva activa del usuario sin descontar
	// el inventario; el listing se consume solo al confirmarse la venta. Si el
	// stock comprable (quantity - reservas activas) no alcanza, devuelve
	// reservation.ErrInsufficientStock.
	Reserve(ctx context.Context, input reservation.ReserveInput, durationMinutes int) (reservation.Reservation, error)
	// ListActiveByUser devuelve las reservas activas del usuario.
	ListActiveByUser(ctx context.Context, userID int64) ([]reservation.Reservation, error)
	// ListActiveByUserAndIDs devuelve reservas activas de un usuario
	// filtrando por ids. Se usa al confirmar la venta.
	ListActiveByUserAndIDs(ctx context.Context, userID int64, ids []int64) ([]reservation.Reservation, error)
	// Confirm marca las reservas indicadas del usuario como 'confirmed'.
	Confirm(ctx context.Context, userID int64, ids []int64) error
	// Remove libera una reserva activa del usuario (el stock queda disponible
	// de nuevo para otros clientes; el inventario no se modifica). Devuelve
	// reservation.ErrNotFound si la reserva no existe, no pertenece al usuario
	// o no está activa.
	Remove(ctx context.Context, userID, id int64) error
	// ReleaseExpired libera todas las reservas 'active' ya vencidas para que
	// su stock vuelva a estar disponible. Devuelve la cantidad de reservas
	// liberadas.
	ReleaseExpired(ctx context.Context) (int64, error)
	// FindListingForReserve devuelve el listing activo más adecuado para
	// reservar de una carta/idioma, opcionalmente filtrando por acabado
	// (variantName).
	FindListingForReserve(ctx context.Context, cardID int64, language, variantName string) (reservation.ListingInfo, error)
	// ListReservationLogs devuelve el historial de reservas (más recientes
	// primero), opcionalmente filtrado por estado. Se usa en el panel admin.
	ListReservationLogs(ctx context.Context, status string, limit, offset int) ([]reservation.ReservationLog, error)
}
