// Package product reúne los casos de uso del módulo de productos propios de la
// tienda. Es independiente del catálogo de cartas: tiene sus propios puertos y
// no comparte tablas con listings ni variantes.
package product

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"trample-back/internal/domain/catalog"
	"trample-back/internal/domain/product"
	"trample-back/internal/ports/out"
)

// ManageUseCase es la administración de productos que hace el admin desde el
// panel: alta, edición, ocultar/publicar, borrar y gestionar las fotos.
type ManageUseCase struct {
	repo out.ProductRepository
}

// NewManageUseCase construye el caso de uso.
func NewManageUseCase(repo out.ProductRepository) *ManageUseCase {
	return &ManageUseCase{repo: repo}
}

// CreateInput es el alta tal como llega del formulario, antes de validarse.
type CreateInput struct {
	Title       string
	Description string
	PriceCOP    int64
	Stock       int
	// Status vacío equivale a publicado.
	Status   string
	Language string
	Game     string
	// ImageURLs y Uploads se mezclan: en total no pueden pasar de MaxImages. Se
	// adjuntan en este orden, URLs primero; la primera queda como principal.
	ImageURLs []string
	Uploads   []catalog.ImageUpload
}

// Create valida, guarda el producto y adjunta sus fotos.
//
// El orden importa: toda la validación (incluidas las URLs y el tope de fotos)
// ocurre antes de escribir, para no dejar un producto a medias por un dato malo.
// Si una foto falla después de crear el producto se devuelve el producto junto
// con ErrImageAttachment: el alta no es idempotente y reenviar el formulario lo
// duplicaría, así que el handler responde 422 con el producto ya creado y el
// front solo debe volver a subir la foto que falló.
func (uc *ManageUseCase) Create(ctx context.Context, adminID string, in CreateInput) (product.Product, error) {
	if adminID == "" {
		return product.Product{}, errors.New("no se pudo identificar al administrador que crea el producto")
	}
	valid, err := product.Input{
		Title:       in.Title,
		Description: in.Description,
		PriceCOP:    in.PriceCOP,
		Stock:       in.Stock,
		Language:    in.Language,
		Game:        in.Game,
	}.Validate()
	if err != nil {
		return product.Product{}, err
	}

	status := in.Status
	if status == "" {
		status = product.StatusActive
	}
	if !product.ValidStatus(status) {
		return product.Product{}, fmt.Errorf("estado inválido: usa %q o %q", product.StatusActive, product.StatusHidden)
	}

	if len(in.ImageURLs)+len(in.Uploads) > product.MaxImages {
		return product.Product{}, product.ErrTooManyImages
	}
	for _, u := range in.ImageURLs {
		if err := product.ValidateImageURL(u); err != nil {
			return product.Product{}, err
		}
	}

	created, err := uc.repo.Create(ctx, adminID, valid, status)
	if err != nil {
		return product.Product{}, err
	}

	for _, u := range in.ImageURLs {
		if _, err := uc.repo.AddImageURL(ctx, created.ID, u); err != nil {
			return uc.withImages(ctx, created), fmt.Errorf("%w: %v", product.ErrImageAttachment, err)
		}
	}
	for _, up := range in.Uploads {
		if _, err := uc.repo.AddImageUpload(ctx, created.ID, up); err != nil {
			return uc.withImages(ctx, created), fmt.Errorf("%w: %v", product.ErrImageAttachment, err)
		}
	}
	return uc.withImages(ctx, created), nil
}

// withImages relee el producto para devolverlo con las fotos que sí quedaron.
func (uc *ManageUseCase) withImages(ctx context.Context, p product.Product) product.Product {
	if fresh, err := uc.repo.GetByID(ctx, p.ID); err == nil {
		return fresh
	}
	return p
}

// Update reemplaza título, descripción, precio y stock.
func (uc *ManageUseCase) Update(ctx context.Context, id int64, in product.Input) (product.Product, error) {
	valid, err := in.Validate()
	if err != nil {
		return product.Product{}, err
	}
	return uc.repo.Update(ctx, id, valid)
}

// BulkUpdate aplica estado y/o juego a los productos seleccionados. Un puntero
// nil deja ese campo intacto; game vacío quita la clasificación.
func (uc *ManageUseCase) BulkUpdate(ctx context.Context, ids []int64, status, game *string) (int, error) {
	if len(ids) == 0 {
		return 0, errors.New("selecciona al menos un producto")
	}
	if len(ids) > 200 {
		return 0, errors.New("selecciona máximo 200 productos a la vez")
	}
	if status != nil {
		if !product.ValidStatus(*status) {
			return 0, fmt.Errorf("estado inválido: usa %q o %q", product.StatusActive, product.StatusHidden)
		}
	}
	if game != nil {
		normalized := strings.TrimSpace(*game)
		if !product.ValidGame(normalized) {
			return 0, fmt.Errorf("el juego debe ser %q, %q, %q o vacío", product.GameMagic, product.GamePokemon, product.GameRiftbound)
		}
		game = &normalized
	}
	return uc.repo.BulkUpdate(ctx, ids, status, game)
}

// SetStatus oculta o publica el producto.
func (uc *ManageUseCase) SetStatus(ctx context.Context, id int64, status string) (product.Product, error) {
	if !product.ValidStatus(status) {
		return product.Product{}, fmt.Errorf("estado inválido: usa %q o %q", product.StatusActive, product.StatusHidden)
	}
	return uc.repo.SetStatus(ctx, id, status)
}

// Delete borra el producto; si ya tiene ventas devuelve product.ErrHasSales.
func (uc *ManageUseCase) Delete(ctx context.Context, id int64) error {
	return uc.repo.Delete(ctx, id)
}

// AddImageURL añade una foto por URL.
func (uc *ManageUseCase) AddImageURL(ctx context.Context, productID int64, url string) (product.Image, error) {
	if err := product.ValidateImageURL(url); err != nil {
		return product.Image{}, err
	}
	return uc.repo.AddImageURL(ctx, productID, url)
}

// AddImageUpload añade una foto subida como archivo.
func (uc *ManageUseCase) AddImageUpload(ctx context.Context, productID int64, upload catalog.ImageUpload) (product.Image, error) {
	return uc.repo.AddImageUpload(ctx, productID, upload)
}

// RemoveImage quita una foto.
func (uc *ManageUseCase) RemoveImage(ctx context.Context, productID, imageID int64) error {
	return uc.repo.RemoveImage(ctx, productID, imageID)
}
