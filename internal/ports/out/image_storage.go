package out

import (
	"context"

	"trample-back/internal/domain/catalog"
)

// ImageStorage guarda los binarios de los frontales fuera de la base de datos.
//
// Existe para que la imagen no dependa de la API: si vive en un bucket, el
// catálogo la carga directo desde el CDN de Supabase y sigue disponible aunque
// el backend esté caído o saturado. También evita meter bytes en PostgreSQL,
// que es lo que peor escala.
type ImageStorage interface {
	// Put sube el frontal y devuelve la URL pública por la que se sirvirá.
	// El implementador decide la clave y no debe reutilizar una por carta:
	// re-subir un frontal dejaría el anterior cacheado en el navegador.
	Put(ctx context.Context, cardID int64, upload catalog.ImageUpload) (string, error)
	// PutProductImage sube una foto de producto. Va en su propio método para que
	// las fotos del módulo de productos no compartan carpeta con las de cartas.
	PutProductImage(ctx context.Context, productID int64, upload catalog.ImageUpload) (string, error)
}
