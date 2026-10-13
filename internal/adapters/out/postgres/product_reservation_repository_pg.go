package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"trample-back/internal/domain/product"
)

// ProductReservationRepositoryPG es el stock temporal de los productos. Sigue
// las mismas reglas que ReservationRepository para las cartas (5 minutos, el
// stock solo se consume al vender, una línea por usuario y producto), pero sobre
// tablas propias.
type ProductReservationRepositoryPG struct {
	db *pgxpool.Pool
}

// NewProductReservationRepositoryPG construye el repositorio.
func NewProductReservationRepositoryPG(db *pgxpool.Pool) *ProductReservationRepositoryPG {
	return &ProductReservationRepositoryPG{db: db}
}

// Reserve aparta unidades del producto en una transacción.
//
// El producto se bloquea con FOR UPDATE para serializar reservas concurrentes:
// sin eso dos clientes podrían reservar la última unidad a la vez.
func (r *ProductReservationRepositoryPG) Reserve(ctx context.Context, in product.ReserveInput, holdMinutes int) (product.Reservation, error) {
	if in.Quantity <= 0 {
		return product.Reservation{}, product.ErrInsufficientStock
	}

	var res product.Reservation
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		var (
			stock    int
			status   string
			title    string
			priceCOP int64
		)
		err := tx.QueryRow(ctx, `
			SELECT stock, status, title, price_cop
			  FROM products WHERE id = $1
			   FOR UPDATE`, in.ProductID).Scan(&stock, &status, &title, &priceCOP)
		if errors.Is(err, pgx.ErrNoRows) {
			return product.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("leer producto: %w", err)
		}
		// Un producto oculto no existe para el cliente.
		if status != product.StatusActive {
			return product.ErrNotFound
		}

		var reserved int
		if err := tx.QueryRow(ctx, `
			SELECT COALESCE(SUM(quantity), 0)::int
			  FROM product_reservations
			 WHERE product_id = $1 AND status = 'active'`, in.ProductID).Scan(&reserved); err != nil {
			return fmt.Errorf("calcular reservado: %w", err)
		}
		if stock-reserved < in.Quantity {
			return product.ErrInsufficientStock
		}

		// Upsert por (usuario, producto) activo: una sola línea en el carrito,
		// con la ventana de 5 minutos renovada. GREATEST para no acortar la
		// retención más larga de un pago en curso.
		err = tx.QueryRow(ctx, `
			INSERT INTO product_reservations (user_id, product_id, quantity, expires_at, status)
			VALUES ($1, $2, $3, now() + make_interval(mins => $4), 'active')
			ON CONFLICT (user_id, product_id) WHERE status = 'active'
			DO UPDATE SET quantity   = product_reservations.quantity + EXCLUDED.quantity,
			              expires_at = GREATEST(product_reservations.expires_at, EXCLUDED.expires_at)
			RETURNING id, quantity, expires_at`,
			in.UserID, in.ProductID, in.Quantity, holdMinutes).Scan(&res.ID, &res.Quantity, &res.ExpiresAt)
		if err != nil {
			return fmt.Errorf("crear o incrementar la reserva: %w", err)
		}

		// Si cambió la cantidad, el pago abierto que incluía esta línea ya no
		// corresponde al carrito.
		if err := closeProductCheckoutsHoldingTx(ctx, tx, res.ID); err != nil {
			return err
		}

		res.UserID = in.UserID
		res.ProductID = in.ProductID
		res.Title = title
		res.PriceCOP = priceCOP
		return tx.QueryRow(ctx, `
			SELECT COALESCE((SELECT url FROM product_images
			                  WHERE product_id = $1 AND url <> ''
			                  ORDER BY position LIMIT 1), '')`, in.ProductID).Scan(&res.ImageURL)
	})
	if err != nil {
		return product.Reservation{}, err
	}
	return res, nil
}

// ListActiveByUser devuelve las reservas vigentes del usuario, la más reciente primero.
func (r *ProductReservationRepositoryPG) ListActiveByUser(ctx context.Context, userID string) ([]product.Reservation, error) {
	return r.listActive(ctx, userID, nil)
}

// ListActiveByUserAndIDs devuelve las reservas vigentes del usuario con esos ids.
func (r *ProductReservationRepositoryPG) ListActiveByUserAndIDs(ctx context.Context, userID string, ids []int64) ([]product.Reservation, error) {
	if len(ids) == 0 {
		return []product.Reservation{}, nil
	}
	return r.listActive(ctx, userID, ids)
}

// listActive lista las reservas vigentes; ids nil significa todas las del usuario.
func (r *ProductReservationRepositoryPG) listActive(ctx context.Context, userID string, ids []int64) ([]product.Reservation, error) {
	rows, err := r.db.Query(ctx, `
		SELECT pr.id, pr.product_id, pr.quantity, pr.expires_at, p.title, p.price_cop,
		       COALESCE((SELECT pi.url FROM product_images pi
		                  WHERE pi.product_id = p.id AND pi.url <> ''
		                  ORDER BY pi.position LIMIT 1), '')
		  FROM product_reservations pr
		  JOIN products p ON p.id = pr.product_id
		 WHERE pr.user_id = $1 AND pr.status = 'active' AND pr.expires_at > now()
		   AND p.status = 'active'
		   AND ($2::bigint[] IS NULL OR pr.id = ANY($2))
		 ORDER BY pr.created_at DESC, pr.id DESC`, userID, ids)
	if err != nil {
		return nil, fmt.Errorf("listar reservas de productos: %w", err)
	}
	defer rows.Close()

	out := []product.Reservation{}
	for rows.Next() {
		res := product.Reservation{UserID: userID}
		if err := rows.Scan(&res.ID, &res.ProductID, &res.Quantity, &res.ExpiresAt,
			&res.Title, &res.PriceCOP, &res.ImageURL); err != nil {
			return nil, fmt.Errorf("escanear reserva de producto: %w", err)
		}
		out = append(out, res)
	}
	return out, rows.Err()
}

// ExtendExpiry renueva la ventana de las reservas activas indicadas. Si alguna ya
// no está activa devuelve product.ErrNotFound.
func (r *ProductReservationRepositoryPG) ExtendExpiry(ctx context.Context, userID string, ids []int64, minutes int) error {
	if len(ids) == 0 {
		return nil
	}
	tag, err := r.db.Exec(ctx, `
		UPDATE product_reservations
		   SET expires_at = now() + make_interval(mins => $3)
		 WHERE user_id = $1 AND id = ANY($2) AND status = 'active'`, userID, ids, minutes)
	if err != nil {
		return fmt.Errorf("extender reservas de productos: %w", err)
	}
	if int(tag.RowsAffected()) != len(ids) {
		return product.ErrNotFound
	}
	return nil
}

// Remove libera la reserva del usuario.
func (r *ProductReservationRepositoryPG) Remove(ctx context.Context, userID string, id int64) error {
	return pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE product_reservations SET status = 'released'
			 WHERE id = $1 AND user_id = $2 AND status = 'active'`, id, userID)
		if err != nil {
			return fmt.Errorf("liberar reserva de producto: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return product.ErrNotFound
		}
		// Quitar la línea cambia el total: el pago abierto que la incluía ya no sirve.
		return closeProductCheckoutsHoldingTx(ctx, tx, id)
	})
}

// ReleaseExpired libera las reservas vencidas. El stock nunca se descontó al
// reservar, así que liberar es solo cambiar el estado.
func (r *ProductReservationRepositoryPG) ReleaseExpired(ctx context.Context) (int64, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE product_reservations SET status = 'released'
		 WHERE status = 'active' AND expires_at <= now()`)
	if err != nil {
		return 0, fmt.Errorf("liberar reservas de productos vencidas: %w", err)
	}
	return tag.RowsAffected(), nil
}

// closeProductCheckoutsHoldingTx cierra los checkouts abiertos que incluyen la
// reserva de producto indicada. No libera reservas: el siguiente "Pagar" crea un
// checkout con el carrito nuevo. (Los checkouts con productos llegan en la fase 3;
// hasta entonces no hay ninguno que cerrar.)
func closeProductCheckoutsHoldingTx(ctx context.Context, tx pgx.Tx, reservationID int64) error {
	if _, err := tx.Exec(ctx, `
		UPDATE payment_checkouts SET status = 'expired'
		 WHERE status = 'open' AND $1 = ANY(product_reservation_ids)`, reservationID); err != nil {
		return fmt.Errorf("cerrar checkouts de la reserva de producto %d: %w", reservationID, err)
	}
	return nil
}
