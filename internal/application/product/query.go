package product

import (
	"context"

	"trample-back/internal/domain/product"
	"trample-back/internal/ports/out"
)

// QueryUseCase lee productos para la tienda (solo publicados) y para el panel
// (todos).
type QueryUseCase struct {
	repo out.ProductRepository
}

// NewQueryUseCase construye el caso de uso.
func NewQueryUseCase(repo out.ProductRepository) *QueryUseCase {
	return &QueryUseCase{repo: repo}
}

// Get devuelve un producto. Para el público un producto oculto no existe
// (ErrNotFound y no ErrHidden): confirmar que existe filtraría un producto que
// el admin decidió no mostrar. El admin sí ve los ocultos.
func (uc *QueryUseCase) Get(ctx context.Context, id int64, admin bool) (product.Product, error) {
	p, err := uc.repo.GetByID(ctx, id)
	if err != nil {
		return product.Product{}, err
	}
	if !admin && p.Status != product.StatusActive {
		return product.Product{}, product.ErrNotFound
	}
	return p, nil
}

// List devuelve una página de productos. Solo el panel puede pedir los ocultos.
func (uc *QueryUseCase) List(ctx context.Context, f product.ListFilter, admin bool) ([]product.Product, int, error) {
	f.IncludeHidden = f.IncludeHidden && admin
	return uc.repo.List(ctx, f)
}
