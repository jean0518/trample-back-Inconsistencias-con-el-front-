package product

import (
	"context"

	"trample-back/internal/domain/product"
	"trample-back/internal/ports/out"
)

// CartUseCase es el carrito de productos: apartar stock por 5 minutos, ver lo
// apartado y soltarlo. Las cartas tienen su propio carrito y el front junta los dos.
type CartUseCase struct {
	repo out.ProductReservationRepository
}

// NewCartUseCase construye el caso de uso.
func NewCartUseCase(repo out.ProductReservationRepository) *CartUseCase {
	return &CartUseCase{repo: repo}
}

// Add aparta N unidades. Si el usuario ya tiene una reserva activa del producto,
// la incrementa y renueva los 5 minutos. Un producto agotado devuelve
// product.ErrInsufficientStock: ese es el "Agotado" del lado servidor.
func (uc *CartUseCase) Add(ctx context.Context, in product.ReserveInput) (product.Reservation, error) {
	if in.UserID == "" || in.ProductID <= 0 || in.Quantity <= 0 {
		return product.Reservation{}, product.ErrNotFound
	}
	return uc.repo.Reserve(ctx, in, product.ReservationHoldMinutes)
}

// List devuelve lo que el usuario tiene apartado.
func (uc *CartUseCase) List(ctx context.Context, userID string) ([]product.Reservation, error) {
	items, err := uc.repo.ListActiveByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []product.Reservation{}
	}
	return items, nil
}

// UpdateQuantity cambia la cantidad de una línea del carrito sin renovar los 5
// minutos, para que editar no sirva para alargar la reserva indefinidamente.
// Bajar libera stock; para quitar la línea está Remove.
func (uc *CartUseCase) UpdateQuantity(ctx context.Context, userID string, id int64, quantity int) (product.Reservation, error) {
	if quantity < 1 {
		return product.Reservation{}, product.ErrInvalidQuantity
	}
	return uc.repo.UpdateQuantity(ctx, userID, id, quantity)
}

// Remove suelta una reserva del usuario.
func (uc *CartUseCase) Remove(ctx context.Context, userID string, id int64) error {
	return uc.repo.Remove(ctx, userID, id)
}
