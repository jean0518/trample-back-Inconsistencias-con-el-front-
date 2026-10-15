package product

import (
	"context"
	"errors"
	"testing"
	"time"

	"trample-back/internal/domain/product"
)

// fakeReservations aplica la misma regla que el repositorio real: lo comprable
// es stock − reservas activas de cualquier usuario.
type fakeReservations struct {
	stock    map[int64]int
	hidden   map[int64]bool
	active   map[int64]*product.Reservation
	nextID   int64
	lastHold int
}

func newFakeReservations() *fakeReservations {
	return &fakeReservations{stock: map[int64]int{}, hidden: map[int64]bool{}, active: map[int64]*product.Reservation{}}
}

func (f *fakeReservations) Reserve(_ context.Context, in product.ReserveInput, hold int) (product.Reservation, error) {
	f.lastHold = hold
	stock, ok := f.stock[in.ProductID]
	if !ok || f.hidden[in.ProductID] {
		return product.Reservation{}, product.ErrNotFound
	}
	reserved := 0
	var mine *product.Reservation
	for _, r := range f.active {
		if r.ProductID == in.ProductID {
			reserved += r.Quantity
			if r.UserID == in.UserID {
				mine = r
			}
		}
	}
	if stock-reserved < in.Quantity {
		return product.Reservation{}, product.ErrInsufficientStock
	}
	if mine != nil {
		mine.Quantity += in.Quantity
		return *mine, nil
	}
	f.nextID++
	r := &product.Reservation{ID: f.nextID, UserID: in.UserID, ProductID: in.ProductID, Quantity: in.Quantity, ExpiresAt: time.Now().Add(time.Duration(hold) * time.Minute)}
	f.active[r.ID] = r
	return *r, nil
}
func (f *fakeReservations) ListActiveByUser(_ context.Context, userID string) ([]product.Reservation, error) {
	var out []product.Reservation
	for _, r := range f.active {
		if r.UserID == userID {
			out = append(out, *r)
		}
	}
	return out, nil
}
func (f *fakeReservations) ListActiveByUserAndIDs(context.Context, string, []int64) ([]product.Reservation, error) {
	return nil, nil
}
func (f *fakeReservations) ExtendExpiry(context.Context, string, []int64, int) error { return nil }
func (f *fakeReservations) Remove(_ context.Context, userID string, id int64) error {
	r, ok := f.active[id]
	if !ok || r.UserID != userID {
		return product.ErrNotFound
	}
	delete(f.active, id)
	return nil
}
func (f *fakeReservations) UpdateQuantity(_ context.Context, userID string, id int64, quantity int) (product.Reservation, error) {
	r, ok := f.active[id]
	if !ok || r.UserID != userID {
		return product.Reservation{}, product.ErrNotFound
	}
	others := 0
	for _, o := range f.active {
		if o.ProductID == r.ProductID && o.ID != id {
			others += o.Quantity
		}
	}
	if quantity > r.Quantity && quantity > f.stock[r.ProductID]-others {
		return product.Reservation{}, product.ErrInsufficientStock
	}
	r.Quantity = quantity
	return *r, nil
}
func (f *fakeReservations) ReleaseExpired(context.Context) (int64, error) { return 0, nil }

func TestCartAddUsesFiveMinuteHold(t *testing.T) {
	repo := newFakeReservations()
	repo.stock[1] = 5
	uc := NewCartUseCase(repo)

	if _, err := uc.Add(context.Background(), product.ReserveInput{UserID: "u1", ProductID: 1, Quantity: 2}); err != nil {
		t.Fatal(err)
	}
	if repo.lastHold != 5 {
		t.Errorf("la reserva debe durar 5 minutos, fue %d", repo.lastHold)
	}
}

func TestCartAddAccumulatesAndRespectsStock(t *testing.T) {
	repo := newFakeReservations()
	repo.stock[1] = 3
	uc := NewCartUseCase(repo)
	ctx := context.Background()

	if _, err := uc.Add(ctx, product.ReserveInput{UserID: "u1", ProductID: 1, Quantity: 2}); err != nil {
		t.Fatal(err)
	}
	// el mismo usuario suma a su línea, no crea otra
	res, err := uc.Add(ctx, product.ReserveInput{UserID: "u1", ProductID: 1, Quantity: 1})
	if err != nil || res.Quantity != 3 {
		t.Fatalf("debía acumular a 3: %+v %v", res, err)
	}
	// ya no queda nada para otro cliente: Agotado
	if _, err := uc.Add(ctx, product.ReserveInput{UserID: "u2", ProductID: 1, Quantity: 1}); !errors.Is(err, product.ErrInsufficientStock) {
		t.Errorf("esperaba ErrInsufficientStock, got %v", err)
	}
	// y u1 tampoco puede pasarse de lo que existe
	if _, err := uc.Add(ctx, product.ReserveInput{UserID: "u1", ProductID: 1, Quantity: 1}); !errors.Is(err, product.ErrInsufficientStock) {
		t.Errorf("no puede exceder el stock: %v", err)
	}
}

func TestCartAddRejectsInvalidAndHidden(t *testing.T) {
	repo := newFakeReservations()
	repo.stock[1] = 5
	repo.stock[2] = 5
	repo.hidden[2] = true
	uc := NewCartUseCase(repo)
	ctx := context.Background()

	bad := []product.ReserveInput{
		{UserID: "", ProductID: 1, Quantity: 1},
		{UserID: "u1", ProductID: 0, Quantity: 1},
		{UserID: "u1", ProductID: 1, Quantity: 0},
		{UserID: "u1", ProductID: 1, Quantity: -2},
		{UserID: "u1", ProductID: 99, Quantity: 1}, // no existe
		{UserID: "u1", ProductID: 2, Quantity: 1},  // oculto
	}
	for i, in := range bad {
		if _, err := uc.Add(ctx, in); !errors.Is(err, product.ErrNotFound) {
			t.Errorf("caso %d: esperaba ErrNotFound, got %v", i, err)
		}
	}
	if len(repo.active) != 0 {
		t.Errorf("no debía reservar nada, hay %d", len(repo.active))
	}
}

func TestCartListAndRemove(t *testing.T) {
	repo := newFakeReservations()
	repo.stock[1] = 5
	uc := NewCartUseCase(repo)
	ctx := context.Background()

	if items, err := uc.List(ctx, "u1"); err != nil || items == nil || len(items) != 0 {
		t.Fatalf("carrito vacío debe ser lista vacía (no nil): %v %v", items, err)
	}
	res, _ := uc.Add(ctx, product.ReserveInput{UserID: "u1", ProductID: 1, Quantity: 1})

	if err := uc.Remove(ctx, "otro", res.ID); !errors.Is(err, product.ErrNotFound) {
		t.Errorf("no puede quitar la reserva de otro: %v", err)
	}
	if err := uc.Remove(ctx, "u1", res.ID); err != nil {
		t.Fatal(err)
	}
	if err := uc.Remove(ctx, "u1", res.ID); !errors.Is(err, product.ErrNotFound) {
		t.Errorf("segunda vez debía ser ErrNotFound: %v", err)
	}
}

func TestCartUpdateQuantityCountsOwnReservation(t *testing.T) {
	repo := newFakeReservations()
	repo.stock[1] = 3
	uc := NewCartUseCase(repo)
	ctx := context.Background()

	mine, _ := uc.Add(ctx, product.ReserveInput{UserID: "u1", ProductID: 1, Quantity: 2})
	// Queda 1 libre: pasar de 2 a 3 solo necesita esa unidad.
	if got, err := uc.UpdateQuantity(ctx, "u1", mine.ID, 3); err != nil || got.Quantity != 3 {
		t.Fatalf("2→3 con 1 libre: %+v %v", got, err)
	}
	if _, err := uc.UpdateQuantity(ctx, "u1", mine.ID, 4); !errors.Is(err, product.ErrInsufficientStock) {
		t.Errorf("4 supera el stock: %v", err)
	}
	// Bajar libera stock para otro cliente.
	if _, err := uc.UpdateQuantity(ctx, "u1", mine.ID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := uc.Add(ctx, product.ReserveInput{UserID: "u2", ProductID: 1, Quantity: 2}); err != nil {
		t.Errorf("las 2 unidades liberadas debían poder reservarse: %v", err)
	}
	if _, err := uc.UpdateQuantity(ctx, "otro", mine.ID, 1); !errors.Is(err, product.ErrNotFound) {
		t.Errorf("no puede editar la reserva de otro: %v", err)
	}
}

func TestCartUpdateQuantityRejectsBelowOne(t *testing.T) {
	repo := newFakeReservations()
	repo.stock[1] = 3
	uc := NewCartUseCase(repo)
	res, _ := uc.Add(context.Background(), product.ReserveInput{UserID: "u1", ProductID: 1, Quantity: 2})

	for _, q := range []int{0, -1} {
		if _, err := uc.UpdateQuantity(context.Background(), "u1", res.ID, q); !errors.Is(err, product.ErrInvalidQuantity) {
			t.Errorf("quantity=%d: esperaba ErrInvalidQuantity, got %v", q, err)
		}
	}
	if repo.active[res.ID].Quantity != 2 {
		t.Errorf("no debía tocar la reserva: %+v", repo.active[res.ID])
	}
}
