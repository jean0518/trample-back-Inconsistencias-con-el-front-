package out

import (
	"context"

	"trample-back/internal/domain/catalog"
)

// CardImageRepository guarda y sirve los frontales de carta subidos como
// archivo, en lugar de referenciar una URL externa.
//
// Las dos formas de aportar una imagen (URL o archivo) alimentan el mismo
// campo del catálogo: card_images.small_url apuntará a un hosting externo en el
// primer caso, o a /card-images/{id} en el segundo.
type CardImageRepository interface {
	// Store persiste los bytes y deja la fila de card_images apuntando al
	// endpoint público. Devuelve el ID del blob, que es el que se usa para
	// construir la URL.
	//
	// cardID y no variantID a propósito: card_images admite exactamente uno de
	// los dos (lo impone un CHECK), y el catálogo solo resuelve la imagen por
	// card_id. Guardarla por variante la volvería invisible.
	Store(ctx context.Context, cardID int64, upload catalog.ImageUpload) (int64, error)
	// Link deja la fila de card_images apuntando a una URL externa, sin bytes
	// en la base. Es el camino del alta manual cuando el staff pega la URL del
	// frontal en vez de subir un archivo: sin esto, la URL se validaba y se
	// devolvía en la respuesta pero la carta se quedaba sin imagen.
	Link(ctx context.Context, cardID int64, externalURL string) error
	// Get devuelve los bytes de una imagen subida. Se usa para servirla desde
	// el endpoint público, sin salir a la red.
	Get(ctx context.Context, id int64) (contentType string, content []byte, err error)
}
