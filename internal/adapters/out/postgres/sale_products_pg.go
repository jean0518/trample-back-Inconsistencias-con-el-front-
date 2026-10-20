package postgres

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"

	"trample-back/internal/domain/product"
	"trample-back/internal/domain/reservation"
	"trample-back/internal/domain/sale"
)

// Este archivo reúne lo que la venta necesita del módulo de productos. Son
// funciones auxiliares que se invocan desde SaleRepository dentro de SUS
// transacciones, para que el pedido de cartas y el de productos se confirmen,
// descuenten o cancelen juntos o no se hagan.

// lockProductStockTx bloquea los productos de la venta y dice si alguno no tiene
// las unidades que se van a vender (o ya no existe). Se bloquean en orden de id
// para que dos ventas simultáneas no se interbloqueen.
func lockProductStockTx(ctx context.Context, tx pgx.Tx, items []sale.ProductItem) (bool, error) {
	need := map[int64]int{}
	short := false
	for _, it := range items {
		if it.ProductID == nil {
			short = true // el producto se eliminó: no hay de dónde descontar
			continue
		}
		need[*it.ProductID] += it.Quantity
	}
	ids := make([]int64, 0, len(need))
	for id := range need {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	for _, id := range ids {
		var stock int
		err := tx.QueryRow(ctx, `SELECT stock FROM products WHERE id = $1 FOR UPDATE`, id).Scan(&stock)
		if errors.Is(err, pgx.ErrNoRows) {
			short = true
			continue
		}
		if err != nil {
			return false, fmt.Errorf("bloquear producto %d: %w", id, err)
		}
		if stock < need[id] {
			short = true
		}
	}
	return short, nil
}

// insertProductItemsTx guarda el snapshot de las líneas de producto de la venta.
//
// product_id se resuelve contra products en el mismo INSERT: si el admin borró el
// producto mientras el cliente pagaba, queda NULL (el título y el precio quedan
// como historial) en vez de violar la clave foránea y dejar un pago cobrado sin pedido.
func insertProductItemsTx(ctx context.Context, tx pgx.Tx, saleID int64, items []sale.ProductItem) ([]sale.ProductItem, error) {
	if len(items) == 0 {
		return nil, nil
	}
	out := make([]sale.ProductItem, 0, len(items))
	for _, it := range items {
		var saved sale.ProductItem
		err := tx.QueryRow(ctx, `
			INSERT INTO sale_product_items (sale_id, product_id, title, quantity, price_cop)
			VALUES ($1, (SELECT id FROM products WHERE id = $2), $3, $4, $5)
			RETURNING product_id, title, quantity, price_cop`,
			saleID, it.ProductID, it.Title, it.Quantity, it.PriceCOP).
			Scan(&saved.ProductID, &saved.Title, &saved.Quantity, &saved.PriceCOP)
		if err != nil {
			return nil, fmt.Errorf("crear item de producto de la venta: %w", err)
		}
		out = append(out, saved)
	}
	return out, nil
}

// decrementProductStockTx descuenta lo vendido. El stock del producto se consume
// aquí, al vender, y no al reservar. Nunca baja de 0: si faltaba stock en un pago
// ya aprobado, la venta queda marcada para revisión (ver insertSale).
func decrementProductStockTx(ctx context.Context, tx pgx.Tx, items []sale.ProductItem) error {
	for _, it := range items {
		if it.ProductID == nil {
			continue
		}
		if _, err := tx.Exec(ctx, `
			UPDATE products
			   SET stock = GREATEST(stock - $2, 0), updated_at = now()
			 WHERE id = $1`, *it.ProductID, it.Quantity); err != nil {
			return fmt.Errorf("descontar producto vendido: %w", err)
		}
	}
	return nil
}

// restoreProductStockTx devuelve al producto las unidades de un pedido cancelado.
// Un producto que se eliminó (product_id NULL) no tiene a dónde volver.
func restoreProductStockTx(ctx context.Context, tx pgx.Tx, saleID int64) error {
	if _, err := tx.Exec(ctx, `
		UPDATE products p
		   SET stock = p.stock + x.qty, updated_at = now()
		  FROM (SELECT product_id, sum(quantity)::int AS qty
		          FROM sale_product_items
		         WHERE sale_id = $1 AND product_id IS NOT NULL
		         GROUP BY product_id) x
		 WHERE p.id = x.product_id`, saleID); err != nil {
		return fmt.Errorf("devolver stock de productos del pedido %d: %w", saleID, err)
	}
	return nil
}

// loadProductItems carga las líneas de producto de una venta.
func (r *SaleRepository) loadProductItems(ctx context.Context, s *sale.Sale) error {
	rows, err := r.db.Query(ctx, `
		SELECT product_id, title, quantity, price_cop
		  FROM sale_product_items WHERE sale_id = $1 ORDER BY id`, s.ID)
	if err != nil {
		return err
	}
	defer rows.Close()

	var items []sale.ProductItem
	for rows.Next() {
		var it sale.ProductItem
		if err := rows.Scan(&it.ProductID, &it.Title, &it.Quantity, &it.PriceCOP); err != nil {
			return err
		}
		items = append(items, it)
	}
	s.ProductItems = items
	return rows.Err()
}

// confirmProductReservationsStrictTx confirma TODAS las reservas de producto del
// usuario o ninguna: si alguna ya no está activa devuelve product.ErrNotFound. Es
// la versión para efectivo/transferencia y recogida, donde todavía no se cobró
// nada y es mejor fallar que vender algo ya no apartado.
func confirmProductReservationsStrictTx(ctx context.Context, tx pgx.Tx, userID string, ids []int64) error {
	if err := lockProductsOfReservationsTx(ctx, tx, ids); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
		UPDATE product_reservations SET status = 'confirmed'
		 WHERE user_id = $1 AND id = ANY($2) AND status = 'active'`, userID, ids)
	if err != nil {
		return fmt.Errorf("confirmar reservas de productos: %w", err)
	}
	if int(tag.RowsAffected()) != len(ids) {
		return product.ErrNotFound
	}
	return logProductReservationsTx(ctx, tx, ids, nil, reservation.LogSold, "")
}

// confirmProductReservationsTx confirma las reservas que sostenían un pago ya
// aprobado. Devuelve true si alguna ya no estaba activa (el stock pudo haberse
// liberado): el dinero entró, así que el pedido se crea igual pero marcado para
// revisión manual.
func confirmProductReservationsTx(ctx context.Context, tx pgx.Tx, ids []int64) (bool, error) {
	if len(ids) == 0 {
		return false, nil
	}
	if err := lockProductsOfReservationsTx(ctx, tx, ids); err != nil {
		return false, err
	}
	confirmed, err := updateProductReservationsTx(ctx, tx, ids, "confirmed")
	if err != nil {
		return false, fmt.Errorf("confirmar reservas de productos del pago: %w", err)
	}
	if err := logProductReservationsTx(ctx, tx, confirmed, nil, reservation.LogSold, ""); err != nil {
		return false, err
	}
	return len(confirmed) < len(ids), nil
}

// releaseProductReservationsTx libera las reservas de producto y deja el
// historial 'returned' con su motivo. Es idempotente: solo toca las que siguen
// activas.
func releaseProductReservationsTx(ctx context.Context, tx pgx.Tx, ids []int64, reason string) error {
	if len(ids) == 0 {
		return nil
	}
	released, err := updateProductReservationsTx(ctx, tx, ids, "released")
	if err != nil {
		return fmt.Errorf("liberar reservas de productos: %w", err)
	}
	return logProductReservationsTx(ctx, tx, released, nil, reservation.LogReturned, reason)
}

// updateProductReservationsTx pasa las reservas activas de `ids` al estado
// indicado y devuelve las que cambió.
func updateProductReservationsTx(ctx context.Context, tx pgx.Tx, ids []int64, status string) ([]int64, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := tx.Query(ctx, `
		UPDATE product_reservations SET status = $2
		 WHERE id = ANY($1) AND status = 'active'
		RETURNING id`, ids, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var changed []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		changed = append(changed, id)
	}
	return changed, rows.Err()
}

// logProductReservationsTx registra un evento del historial para cada reserva de
// producto de `ids`, con los datos del cliente y del producto en ese momento.
// `quantity` nil usa la cantidad actual de cada reserva (vender, devolver todo);
// con valor, es la cantidad del evento (sumar o quitar unidades).
func logProductReservationsTx(ctx context.Context, tx pgx.Tx, ids []int64, quantity *int, status, reason string) error {
	if len(ids) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO product_reservation_logs
			(reservation_id, user_id, customer_name, customer_email,
			 product_id, title, quantity, price_cop, status, return_reason)
		SELECT pr.id, pr.user_id,
		       TRIM(COALESCE(u.first_name, '') || ' ' || COALESCE(u.last_name, '')), COALESCE(u.email, ''),
		       p.id, p.title, COALESCE($2::int, pr.quantity), p.price_cop, $3, NULLIF($4, '')
		FROM product_reservations pr
		JOIN products p ON p.id = pr.product_id
		LEFT JOIN users u ON u.id = pr.user_id
		WHERE pr.id = ANY($1)`, ids, quantity, status, reason); err != nil {
		return fmt.Errorf("registrar historial de reservas de productos: %w", err)
	}
	return nil
}

// lockProductsOfReservationsTx bloquea, en orden de id, los productos de esas
// reservas ANTES de tocar las reservas. Reserve toma primero el producto y luego
// la fila de su reserva; si la venta hiciera el recorrido inverso, un "Pagar" y un
// "Agregar al carrito" simultáneos del mismo cliente podrían interbloquearse.
func lockProductsOfReservationsTx(ctx context.Context, tx pgx.Tx, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `
		SELECT id FROM products
		 WHERE id IN (SELECT product_id FROM product_reservations WHERE id = ANY($1))
		 ORDER BY id
		   FOR UPDATE`, ids)
	if err != nil {
		return fmt.Errorf("bloquear productos de las reservas: %w", err)
	}
	rows.Close()
	return rows.Err()
}
