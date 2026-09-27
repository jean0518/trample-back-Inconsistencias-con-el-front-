package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"trample-back/internal/domain/catalog"
	"trample-back/internal/ports/out"
)

// CardImageRepositoryPG guarda los frontales subidos como archivo y mantiene
// card_images apuntando a la dirección donde se sirven.
//
// Los bytes van a Supabase Storage cuando hay bucket configurado, y a la tabla
// card_image_blobs cuando no. El respaldo existe porque los dos caminos
// conviven: las imágenes ya subidas siguen en la base y se sirven por
// /card-images/{id}, así que quitar el bucket no puede dejarlas huérfanas.
type CardImageRepositoryPG struct {
	db      *pgxpool.Pool
	storage out.ImageStorage
}

// NewCardImageRepositoryPG construye el repositorio sobre el pool de pgx.
// storage puede ser nil, y en ese caso los frontales se guardan en la base.
func NewCardImageRepositoryPG(db *pgxpool.Pool, storage out.ImageStorage) *CardImageRepositoryPG {
	return &CardImageRepositoryPG{db: db, storage: storage}
}

// upsertImage es el enlace hacia el catálogo que comparten Store y Link: la
// imagen vive a nivel de carta (card_id informado, variant_id NULL) y no por
// variante, porque card_images tiene un CHECK que exige exactamente uno de los
// dos y el catálogo resuelve el frontal solo con WHERE card_id = c.id.
const upsertImage = `
	INSERT INTO card_images (card_id, image_type, small_url, medium_url, large_url)
	VALUES ($1, 'front', $2, $2, $2)
	ON CONFLICT (card_id, variant_id, image_type) DO UPDATE
		   SET small_url  = EXCLUDED.small_url,
		       medium_url = EXCLUDED.medium_url,
		       large_url  = EXCLUDED.large_url`

// Store persiste los bytes y, en la misma transacción, actualiza la fila de
// card_images para que el catálogo muestre el nuevo frontal. La transacción
// importa: si el blob quedara guardado y card_images no, el catálogo seguiría
// mostrando la imagen anterior sin que nada avise del desajuste.
//
// NULLS NOT DISTINCT (ver migración 013) es imprescindible en este upsert:
// como variant_id siempre es NULL en estas filas, un índice único normal no
// haría que NULL colisionara consigo mismo y el ON CONFLICT nunca encontraría
// la fila previa, de modo que cada re-subida insertaría una duplicada en vez de
// reemplazar el frontal.
func (r *CardImageRepositoryPG) Store(ctx context.Context, cardID int64, upload catalog.ImageUpload) (string, error) {
	if upload.IsEmpty() {
		return "", fmt.Errorf("la imagen está vacía")
	}

	// Camino preferido: el bucket. La subida va primero porque no se puede
	// deshacer: si el UPDATE de card_images fallara con el archivo ya subido,
	// lo que queda es un objeto huérfano en el bucket, que es inocuo, mientras
	// que al revés la carta apuntaría a una imagen que no existe.
	if r.storage != nil {
		publicURL, err := r.storage.Put(ctx, cardID, upload)
		if err != nil {
			return "", err
		}
		if _, err := r.db.Exec(ctx, upsertImage, cardID, publicURL); err != nil {
			return "", fmt.Errorf("no se pudo enlazar la imagen con la carta: %w", err)
		}
		return publicURL, nil
	}

	var imageID int64
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		const insertBlob = `
			INSERT INTO card_image_blobs (card_id, content_type, bytes, size_bytes)
			VALUES ($1, $2, $3, $4)
			RETURNING id`
		if err := tx.QueryRow(ctx, insertBlob, cardID, upload.ContentType, upload.Content, upload.SizeBytes()).Scan(&imageID); err != nil {
			return fmt.Errorf("no se pudo guardar la imagen: %w", err)
		}

		if _, err := tx.Exec(ctx, upsertImage, cardID, fmt.Sprintf("/card-images/%d", imageID)); err != nil {
			return fmt.Errorf("no se pudo enlazar la imagen con la carta: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("/card-images/%d", imageID), nil
}

// Link apunta el frontal de la carta a una URL externa. No copia los bytes: la
// imagen sigue servida por su hosting y el catálogo la referencia igual que a
// las que trae Scrydex.
func (r *CardImageRepositoryPG) Link(ctx context.Context, cardID int64, externalURL string) error {
	if externalURL == "" {
		return fmt.Errorf("la URL de la imagen está vacía")
	}
	if _, err := r.db.Exec(ctx, upsertImage, cardID, externalURL); err != nil {
		return fmt.Errorf("no se pudo enlazar la imagen con la carta: %w", err)
	}
	return nil
}

// Get devuelve los bytes de una imagen subida para servirla. No filtra por el
// estado de la carta: el endpoint es público y la imagen debe seguir cargando
// aunque la carta esté pausada o sin stock (por ejemplo, en la galería).
func (r *CardImageRepositoryPG) Get(ctx context.Context, id int64) (string, []byte, error) {
	const q = `SELECT content_type, bytes FROM card_image_blobs WHERE id = $1`

	var contentType string
	var content []byte
	err := r.db.QueryRow(ctx, q, id).Scan(&contentType, &content)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, catalog.ErrNotFound
	}
	if err != nil {
		return "", nil, fmt.Errorf("no se pudo leer la imagen: %w", err)
	}
	return contentType, content, nil
}
