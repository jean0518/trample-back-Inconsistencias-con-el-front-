package out

import (
	"context"

	"trample-back/internal/domain/product"
)

// ProductReservationRepository gestiona el stock temporal (5 min) de los
// productos en el carrito. Es el espejo de ReservationRepository para el módulo
// de productos, que no comparte tablas con las cartas.
type ProductReservationRepository interface {
	// Reserve crea (o incrementa) la reserva activa del usuario sin descontar el
	// stock. Devuelve product.ErrNotFound si el producto no existe o está oculto,
	// y product.ErrInsufficientStock si lo comprable (stock − reservas activas de
	// cualquier usuario) no alcanza.
	Reserve(ctx context.Context, in product.ReserveInput, holdMinutes int) (product.Reservation, error)
	// ListActiveByUser devuelve las reservas vigentes del usuario.
	ListActiveByUser(ctx context.Context, userID string) ([]product.Reservation, error)
	// ListActiveByUserAndIDs devuelve las reservas vigentes del usuario con esos
	// ids. Se usa al cobrar: si devuelve menos de las pedidas, alguna venció.
	ListActiveByUserAndIDs(ctx context.Context, userID string, ids []int64) ([]product.Reservation, error)
	// ExtendExpiry renueva a `minutes` desde ahora la ventana de las reservas
	// activas indicadas (la retención de 5 min del carrito pasa a la del pago).
	// Devuelve product.ErrNotFound si alguna ya no está activa.
	ExtendExpiry(ctx context.Context, userID string, ids []int64, minutes int) error
	// Remove libera una reserva activa del usuario. Devuelve product.ErrNotFound
	// si no existe, no es suya o ya no está activa.
	Remove(ctx context.Context, userID string, id int64) error
	// UpdateQuantity fija la cantidad de una reserva activa y vigente del
	// usuario, conservando su expires_at. Para subir valida contra lo comprable
	// sumando lo que la propia reserva ya aparta. Devuelve product.ErrNotFound si
	// no existe, no es suya, venció o el producto está oculto;
	// product.ErrCheckoutInProgress si está en un pago abierto; y
	// product.ErrInsufficientStock si no alcanza.
	UpdateQuantity(ctx context.Context, userID string, id int64, quantity int) (product.Reservation, error)
	// ReleaseExpired libera las reservas activas ya vencidas y devuelve cuántas.
	ReleaseExpired(ctx context.Context) (int64, error)
}
