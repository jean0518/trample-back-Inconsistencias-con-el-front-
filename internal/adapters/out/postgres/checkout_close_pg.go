package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Cuando el inventario cambia mientras un cliente paga, el checkout abierto de
// Bold queda desactualizado (precio, stock u oferta distintos). Estas funciones
// lo cierran, igual que closeCheckoutsHoldingTx hace al cambiar el carrito: no
// liberan reservas, el siguiente "Pagar" crea un checkout con los datos nuevos.
// Si el cliente igual paga el modal viejo, el pedido se crea y queda marcado
// para revisión (ApprovePayment).

// closeCheckoutsOfListingTx cierra los checkouts abiertos con reservas activas
// de ese listing.
func closeCheckoutsOfListingTx(ctx context.Context, tx pgx.Tx, listingID int64) error {
	if _, err := tx.Exec(ctx, `
		UPDATE payment_checkouts SET status = 'expired'
		WHERE status = 'open'
		  AND reservation_ids && COALESCE(
		        (SELECT array_agg(id) FROM cart_reservations WHERE listing_id = $1 AND status = 'active'),
		        '{}'::bigint[])
	`, listingID); err != nil {
		return fmt.Errorf("cerrar checkouts del listing %d: %w", listingID, err)
	}
	return nil
}

// closeCheckoutsIfStockBelowReservedTx cierra los checkouts del listing solo si
// las unidades reservadas ya no caben en `quantity`.
func closeCheckoutsIfStockBelowReservedTx(ctx context.Context, tx pgx.Tx, listingID int64, quantity int) error {
	var reserved int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(quantity), 0)::int FROM cart_reservations
		WHERE listing_id = $1 AND status = 'active'
	`, listingID).Scan(&reserved); err != nil {
		return fmt.Errorf("calcular reservado del listing %d: %w", listingID, err)
	}
	if reserved > quantity {
		return closeCheckoutsOfListingTx(ctx, tx, listingID)
	}
	return nil
}

// closeCheckoutsOfProductTx cierra los checkouts abiertos con reservas activas
// de ese producto.
func closeCheckoutsOfProductTx(ctx context.Context, tx pgx.Tx, productID int64) error {
	if _, err := tx.Exec(ctx, `
		UPDATE payment_checkouts SET status = 'expired'
		WHERE status = 'open'
		  AND product_reservation_ids && COALESCE(
		        (SELECT array_agg(id) FROM product_reservations WHERE product_id = $1 AND status = 'active'),
		        '{}'::bigint[])
	`, productID); err != nil {
		return fmt.Errorf("cerrar checkouts del producto %d: %w", productID, err)
	}
	return nil
}

// closeProductCheckoutsIfStockBelowReservedTx es el equivalente de
// closeCheckoutsIfStockBelowReservedTx para productos.
func closeProductCheckoutsIfStockBelowReservedTx(ctx context.Context, tx pgx.Tx, productID int64, stock int) error {
	var reserved int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(quantity), 0)::int FROM product_reservations
		WHERE product_id = $1 AND status = 'active'
	`, productID).Scan(&reserved); err != nil {
		return fmt.Errorf("calcular reservado del producto %d: %w", productID, err)
	}
	if reserved > stock {
		return closeCheckoutsOfProductTx(ctx, tx, productID)
	}
	return nil
}
