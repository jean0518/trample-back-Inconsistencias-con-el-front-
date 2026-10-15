package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"trample-back/internal/domain/catalog"
	"trample-back/internal/domain/product"
	"trample-back/internal/ports/out"
)

// ProductRepositoryPG persiste los productos propios de la tienda.
//
// Las fotos van a Supabase Storage cuando hay bucket configurado y, si no, a la
// propia tabla product_images (mismo criterio que los frontales de cartas, pero
// con tablas y carpeta propias).
type ProductRepositoryPG struct {
	db      *pgxpool.Pool
	storage out.ImageStorage
}

// NewProductRepositoryPG construye el repositorio. storage puede ser nil.
func NewProductRepositoryPG(db *pgxpool.Pool, storage out.ImageStorage) *ProductRepositoryPG {
	return &ProductRepositoryPG{db: db, storage: storage}
}

// productColumns proyecta el producto con las unidades que hoy sostienen
// reservas activas. El reservado se calcula aquí y no se guarda: cambia con cada
// carrito y guardarlo obligaría a mantener dos fuentes de verdad.
const productColumns = `
	p.id, p.title, p.description, p.price_cop, p.stock, p.status,
	COALESCE((SELECT sum(pr.quantity) FROM product_reservations pr
	          WHERE pr.product_id = p.id AND pr.status = 'active'), 0)::int AS reserved,
	p.created_at, p.updated_at`

func scanProduct(row pgx.Row) (product.Product, error) {
	var p product.Product
	err := row.Scan(&p.ID, &p.Title, &p.Description, &p.PriceCOP, &p.Stock, &p.Status,
		&p.Reserved, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

// Create inserta el producto.
func (r *ProductRepositoryPG) Create(ctx context.Context, createdBy string, in product.Input, status string) (product.Product, error) {
	var id int64
	err := r.db.QueryRow(ctx, `
		INSERT INTO products (title, description, price_cop, stock, status, created_by)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, '')::uuid)
		RETURNING id`,
		in.Title, in.Description, in.PriceCOP, in.Stock, status, createdBy).Scan(&id)
	if err != nil {
		return product.Product{}, fmt.Errorf("crear producto: %w", err)
	}
	return r.GetByID(ctx, id)
}

// Update reemplaza los campos editables. Si cambia el precio, o el stock deja de
// cubrir las reservas, cierra los pagos abiertos que incluían el producto: Bold
// iba a cobrar un monto o unas unidades que ya no corresponden.
func (r *ProductRepositoryPG) Update(ctx context.Context, id int64, in product.Input) (product.Product, error) {
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		var oldPrice int64
		if err := tx.QueryRow(ctx, `SELECT price_cop FROM products WHERE id = $1 FOR UPDATE`, id).Scan(&oldPrice); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return product.ErrNotFound
			}
			return fmt.Errorf("bloquear producto: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE products
			   SET title = $2, description = $3, price_cop = $4, stock = $5, updated_at = now()
			 WHERE id = $1`,
			id, in.Title, in.Description, in.PriceCOP, in.Stock); err != nil {
			return fmt.Errorf("actualizar producto: %w", err)
		}
		if in.PriceCOP != oldPrice {
			return closeCheckoutsOfProductTx(ctx, tx, id)
		}
		return closeProductCheckoutsIfStockBelowReservedTx(ctx, tx, id, in.Stock)
	})
	if err != nil {
		return product.Product{}, err
	}
	return r.GetByID(ctx, id)
}

// SetStatus oculta o publica el producto. Ocultarlo cierra los pagos abiertos
// que lo incluían: un producto oculto ya no se puede vender.
func (r *ProductRepositoryPG) SetStatus(ctx context.Context, id int64, status string) (product.Product, error) {
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE products SET status = $2, updated_at = now() WHERE id = $1`, id, status)
		if err != nil {
			return fmt.Errorf("cambiar estado del producto: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return product.ErrNotFound
		}
		if status != product.StatusActive {
			return closeCheckoutsOfProductTx(ctx, tx, id)
		}
		return nil
	})
	if err != nil {
		return product.Product{}, err
	}
	return r.GetByID(ctx, id)
}

// Delete borra el producto salvo que ya tenga ventas. Se revisa dentro de la
// transacción y con el producto bloqueado para que una venta concurrente no se
// cuele entre la comprobación y el borrado.
func (r *ProductRepositoryPG) Delete(ctx context.Context, id int64) error {
	return pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT true FROM products WHERE id = $1 FOR UPDATE`, id).Scan(&exists); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return product.ErrNotFound
			}
			return fmt.Errorf("bloquear producto: %w", err)
		}

		var sold bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM sale_product_items WHERE product_id = $1)`, id).Scan(&sold); err != nil {
			return fmt.Errorf("revisar ventas del producto: %w", err)
		}
		if sold {
			return product.ErrHasSales
		}

		// Un pago abierto con este producto ya no se puede cumplir: se cierra
		// antes de borrar las reservas que lo sostenían.
		if err := closeCheckoutsOfProductTx(ctx, tx, id); err != nil {
			return err
		}

		// Las reservas ya no tienen sentido sin producto. Las activas se borran
		// junto con él: el carrito de ese cliente simplemente deja de listarlo.
		if _, err := tx.Exec(ctx, `DELETE FROM product_reservations WHERE product_id = $1`, id); err != nil {
			return fmt.Errorf("borrar reservas del producto: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM products WHERE id = $1`, id); err != nil {
			return fmt.Errorf("borrar producto: %w", err)
		}
		return nil
	})
}

// GetByID devuelve el producto con sus fotos.
func (r *ProductRepositoryPG) GetByID(ctx context.Context, id int64) (product.Product, error) {
	p, err := scanProduct(r.db.QueryRow(ctx, `SELECT `+productColumns+` FROM products p WHERE p.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return product.Product{}, product.ErrNotFound
	}
	if err != nil {
		return product.Product{}, fmt.Errorf("leer producto: %w", err)
	}
	images, err := r.imagesFor(ctx, []int64{id})
	if err != nil {
		return product.Product{}, err
	}
	p.Images = images[id]
	return p, nil
}

// List devuelve una página de productos, del más nuevo al más viejo.
func (r *ProductRepositoryPG) List(ctx context.Context, f product.ListFilter) ([]product.Product, int, error) {
	f = f.Normalize()

	where := []string{"TRUE"}
	args := []any{}
	if !f.IncludeHidden {
		where = append(where, "p.status = 'active'")
	}
	if f.Query != "" {
		args = append(args, "%"+escapeLike(f.Query)+"%")
		where = append(where, fmt.Sprintf("p.title ILIKE $%d", len(args)))
	}
	cond := strings.Join(where, " AND ")

	var total int
	if err := r.db.QueryRow(ctx, `SELECT count(*) FROM products p WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("contar productos: %w", err)
	}

	args = append(args, f.Limit, f.Offset)
	rows, err := r.db.Query(ctx, fmt.Sprintf(
		`SELECT %s FROM products p WHERE %s ORDER BY p.created_at DESC, p.id DESC LIMIT $%d OFFSET $%d`,
		productColumns, cond, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("listar productos: %w", err)
	}
	defer rows.Close()

	items := make([]product.Product, 0, f.Limit)
	ids := make([]int64, 0, f.Limit)
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("escanear producto: %w", err)
		}
		items = append(items, p)
		ids = append(ids, p.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	images, err := r.imagesFor(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	for i := range items {
		items[i].Images = images[items[i].ID]
	}
	return items, total, nil
}

// escapeLike neutraliza los comodines de LIKE para que buscar "100%" no
// devuelva todo el catálogo.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// imagesFor trae las fotos de varios productos en una sola consulta.
func (r *ProductRepositoryPG) imagesFor(ctx context.Context, ids []int64) (map[int64][]product.Image, error) {
	out := make(map[int64][]product.Image, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(ctx, `
		SELECT product_id, id, position, url
		  FROM product_images
		 WHERE product_id = ANY($1)
		 ORDER BY product_id, position`, ids)
	if err != nil {
		return nil, fmt.Errorf("leer fotos de productos: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var productID int64
		var img product.Image
		if err := rows.Scan(&productID, &img.ID, &img.Position, &img.URL); err != nil {
			return nil, fmt.Errorf("escanear foto: %w", err)
		}
		out[productID] = append(out[productID], img)
	}
	return out, rows.Err()
}

// AddImageURL añade una foto externa.
func (r *ProductRepositoryPG) AddImageURL(ctx context.Context, productID int64, url string) (product.Image, error) {
	if strings.TrimSpace(url) == "" {
		return product.Image{}, errors.New("la URL de la imagen está vacía")
	}
	var img product.Image
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		position, err := nextImagePosition(ctx, tx, productID)
		if err != nil {
			return err
		}
		img.Position = position
		img.URL = url
		return tx.QueryRow(ctx, `
			INSERT INTO product_images (product_id, position, url)
			VALUES ($1, $2, $3) RETURNING id`, productID, position, url).Scan(&img.ID)
	})
	if err != nil {
		return product.Image{}, err
	}
	return img, nil
}

// AddImageUpload guarda los bytes y añade la foto.
//
// Con bucket, la subida va primero porque no se puede deshacer: si el INSERT
// fallara con el archivo ya subido, queda un objeto huérfano en el bucket (inocuo)
// en vez de una fila apuntando a un archivo que no existe.
func (r *ProductRepositoryPG) AddImageUpload(ctx context.Context, productID int64, upload catalog.ImageUpload) (product.Image, error) {
	if upload.IsEmpty() {
		return product.Image{}, errors.New("la imagen está vacía")
	}

	var publicURL string
	if r.storage != nil {
		// Se comprueba el cupo antes de subir para no dejar objetos huérfanos
		// cuando el producto ya tiene 5 fotos. La comprobación definitiva (con
		// el producto bloqueado) sigue siendo la de la transacción.
		if _, err := nextImagePosition(ctx, r.db, productID); err != nil {
			return product.Image{}, err
		}
		url, err := r.storage.PutProductImage(ctx, productID, upload)
		if err != nil {
			return product.Image{}, err
		}
		publicURL = url
	}

	var img product.Image
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		position, err := nextImagePosition(ctx, tx, productID)
		if err != nil {
			return err
		}
		img.Position = position

		if publicURL != "" {
			img.URL = publicURL
			return tx.QueryRow(ctx, `
				INSERT INTO product_images (product_id, position, url)
				VALUES ($1, $2, $3) RETURNING id`, productID, position, publicURL).Scan(&img.ID)
		}

		// Sin bucket: los bytes van a la tabla y la URL se arma con el id.
		if err := tx.QueryRow(ctx, `
			INSERT INTO product_images (product_id, position, url, content_type, bytes, size_bytes)
			VALUES ($1, $2, '', $3, $4, $5) RETURNING id`,
			productID, position, upload.ContentType, upload.Content, upload.SizeBytes()).Scan(&img.ID); err != nil {
			return err
		}
		img.URL = fmt.Sprintf("/product-images/%d", img.ID)
		_, err = tx.Exec(ctx, `UPDATE product_images SET url = $2 WHERE id = $1`, img.ID, img.URL)
		return err
	})
	if err != nil {
		return product.Image{}, err
	}
	return img, nil
}

// queryRower es lo común entre el pool y una transacción.
type queryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// nextImagePosition devuelve la siguiente posición libre (1..5). Dentro de una
// transacción bloquea la fila del producto, de modo que dos altas simultáneas
// no calculen la misma posición ni superen el máximo de fotos.
func nextImagePosition(ctx context.Context, q queryRower, productID int64) (int, error) {
	var exists bool
	lock := `SELECT true FROM products WHERE id = $1`
	if _, inTx := q.(pgx.Tx); inTx {
		lock += ` FOR UPDATE`
	}
	if err := q.QueryRow(ctx, lock, productID).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, product.ErrNotFound
		}
		return 0, fmt.Errorf("leer producto: %w", err)
	}

	var count, max int
	if err := q.QueryRow(ctx,
		`SELECT count(*)::int, COALESCE(max(position), 0)::int FROM product_images WHERE product_id = $1`,
		productID).Scan(&count, &max); err != nil {
		return 0, fmt.Errorf("contar fotos: %w", err)
	}
	if count >= product.MaxImages {
		return 0, product.ErrTooManyImages
	}
	// Con huecos (por fotos quitadas sin reordenar) se usa el primer hueco; RemoveImage
	// compacta las posiciones, así que max+1 == count+1 y esto es solo una red de seguridad.
	if max >= product.MaxImages {
		return 0, product.ErrTooManyImages
	}
	return max + 1, nil
}

// RemoveImage quita la foto y compacta las posiciones para que la principal
// siga siendo la 1 y no queden huecos.
func (r *ProductRepositoryPG) RemoveImage(ctx context.Context, productID, imageID int64) error {
	return pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM products WHERE id = $1 FOR UPDATE`, productID); err != nil {
			return fmt.Errorf("bloquear producto: %w", err)
		}
		var removed int
		err := tx.QueryRow(ctx,
			`DELETE FROM product_images WHERE id = $1 AND product_id = $2 RETURNING position`,
			imageID, productID).Scan(&removed)
		if errors.Is(err, pgx.ErrNoRows) {
			return product.ErrImageNotFound
		}
		if err != nil {
			return fmt.Errorf("borrar foto: %w", err)
		}
		// Las posteriores suben un lugar. El UNIQUE (product_id, position) es
		// DEFERRABLE INITIALLY IMMEDIATE, así que se comprueba al final de la
		// sentencia y no fila a fila: sin eso 3→2 chocaría con la 2 que aún no se movió.
		if _, err := tx.Exec(ctx,
			`UPDATE product_images SET position = position - 1 WHERE product_id = $1 AND position > $2`,
			productID, removed); err != nil {
			return fmt.Errorf("reordenar fotos: %w", err)
		}
		return nil
	})
}

// GetImage devuelve los bytes de una foto guardada en la base.
func (r *ProductRepositoryPG) GetImage(ctx context.Context, id int64) (string, []byte, error) {
	var contentType *string
	var content []byte
	err := r.db.QueryRow(ctx, `SELECT content_type, bytes FROM product_images WHERE id = $1`, id).Scan(&contentType, &content)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && len(content) == 0) {
		return "", nil, product.ErrImageNotFound
	}
	if err != nil {
		return "", nil, fmt.Errorf("leer foto: %w", err)
	}
	ct := "application/octet-stream"
	if contentType != nil {
		ct = *contentType
	}
	return ct, content, nil
}
