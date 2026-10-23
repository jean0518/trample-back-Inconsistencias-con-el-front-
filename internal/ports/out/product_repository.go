package out

import (
	"context"

	"trample-back/internal/domain/catalog"
	"trample-back/internal/domain/product"
)

// ProductRepository persiste los productos propios de la tienda y sus fotos.
type ProductRepository interface {
	// Create guarda el producto ya validado. createdBy es el admin que lo crea.
	Create(ctx context.Context, createdBy string, in product.Input, status string) (product.Product, error)
	// Update reemplaza los datos editables. Devuelve product.ErrNotFound si no existe.
	Update(ctx context.Context, id int64, in product.Input) (product.Product, error)
	// SetStatus oculta o publica el producto.
	SetStatus(ctx context.Context, id int64, status string) (product.Product, error)
	// BulkUpdate aplica estado y/o juego a varios productos. Nil = no cambiar.
	// Devuelve cuántos productos se actualizaron.
	BulkUpdate(ctx context.Context, ids []int64, status, game *string) (int, error)
	// Delete borra el producto y sus fotos. Devuelve product.ErrHasSales si ya
	// se vendió: el historial de ventas no se toca nunca.
	Delete(ctx context.Context, id int64) error

	// GetByID devuelve el producto con fotos y disponibilidad. No filtra por
	// estado: decidir si un oculto se muestra es del caso de uso.
	GetByID(ctx context.Context, id int64) (product.Product, error)
	// List devuelve una página y el total que cumple el filtro.
	List(ctx context.Context, f product.ListFilter) ([]product.Product, int, error)

	// AddImageURL añade una foto que apunta a una URL externa. Devuelve
	// product.ErrTooManyImages si ya tiene 5.
	AddImageURL(ctx context.Context, productID int64, url string) (product.Image, error)
	// AddImageUpload guarda los bytes (bucket de Supabase o base) y añade la
	// foto. Mismo límite de 5.
	AddImageUpload(ctx context.Context, productID int64, upload catalog.ImageUpload) (product.Image, error)
	// RemoveImage quita una foto del producto y reordena las restantes.
	RemoveImage(ctx context.Context, productID, imageID int64) error
	// GetImage devuelve los bytes de una foto guardada en la base, para servirla
	// desde /product-images/{id}.
	GetImage(ctx context.Context, id int64) (contentType string, content []byte, err error)
}
