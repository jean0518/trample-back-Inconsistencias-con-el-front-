package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"trample-back/internal/domain/reservation"
	"trample-back/internal/domain/sale"
)

type orderCardLine struct {
	id        int64
	listingID *int64
	quantity  int
}

type orderProductLine struct {
	id        int64
	productID *int64
	quantity  int
}

// UpdateOrder corrige un pedido existente: datos de entrega, líneas, precios y
// estado. Los ítems enviados reemplazan a los actuales, ajustando el stock por
// la diferencia. Cuando el pedido pasa a cancelado se devuelve todo el stock.
func (r *SaleRepository) UpdateOrder(ctx context.Context, in sale.UpdateOrderInput) (sale.Sale, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return sale.Sale{}, err
	}
	defer tx.Rollback(ctx)

	var (
		currentStatus string
		shippingCOP   int64
	)
	err = tx.QueryRow(ctx, `
		SELECT status, shipping_cop FROM sales WHERE id = $1 FOR UPDATE
	`, in.ID).Scan(&currentStatus, &shippingCOP)
	if errors.Is(err, pgx.ErrNoRows) {
		return sale.Sale{}, sale.ErrPaymentNotFound
	}
	if err != nil {
		return sale.Sale{}, fmt.Errorf("buscar pedido %d para editar: %w", in.ID, err)
	}
	if currentStatus == sale.StatusCancelled {
		return sale.Sale{}, sale.ErrOrderNotEditable
	}

	targetStatus := in.Status
	if targetStatus == "" {
		targetStatus = currentStatus
	}
	if err := validateOrderTransition(currentStatus, targetStatus); err != nil {
		return sale.Sale{}, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE sales SET address = $2, city = $3, phone = $4 WHERE id = $1
	`, in.ID, in.Address, in.City, in.Phone); err != nil {
		return sale.Sale{}, fmt.Errorf("actualizar datos de entrega del pedido %d: %w", in.ID, err)
	}

	if in.Items != nil {
		if err := updateOrderCardsTx(ctx, tx, in.ID, in.Items); err != nil {
			return sale.Sale{}, err
		}
	}
	if in.ProductItems != nil {
		if err := updateOrderProductsTx(ctx, tx, in.ID, in.ProductItems); err != nil {
			return sale.Sale{}, err
		}
	}

	var cardsCOP, productsCOP, cardsUSD, productsUSD float64
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(quantity * price_cop), 0)::float8,
		       COALESCE(SUM(quantity * price_usd), 0)::float8
		  FROM sale_items WHERE sale_id = $1
	`, in.ID).Scan(&cardsCOP, &cardsUSD); err != nil {
		return sale.Sale{}, fmt.Errorf("recalcular cartas del pedido %d: %w", in.ID, err)
	}
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(quantity * price_cop), 0)::float8
		  FROM sale_product_items WHERE sale_id = $1
	`, in.ID).Scan(&productsCOP); err != nil {
		return sale.Sale{}, fmt.Errorf("recalcular productos del pedido %d: %w", in.ID, err)
	}
	totalCOP := int64(cardsCOP+productsCOP) + shippingCOP
	totalUSD := cardsUSD + productsUSD
	if _, err := tx.Exec(ctx, `UPDATE sales SET total_cop = $2, total_usd = $3 WHERE id = $1`, in.ID, totalCOP, totalUSD); err != nil {
		return sale.Sale{}, fmt.Errorf("actualizar totales del pedido %d: %w", in.ID, err)
	}

	// Un pedido con crédito mantiene la deuda alineada con el nuevo total. Si el
	// nuevo total quedara por debajo de lo ya abonado, se rechaza la edición.
	if currentStatus == sale.StatusCredit {
		var paid int64
		if err := tx.QueryRow(ctx, `SELECT paid_cop FROM store_credits WHERE sale_id = $1 FOR UPDATE`, in.ID).Scan(&paid); err != nil {
			return sale.Sale{}, fmt.Errorf("buscar crédito del pedido %d: %w", in.ID, err)
		}
		if totalCOP < paid {
			return sale.Sale{}, sale.ErrCreditPayment
		}
		if _, err := tx.Exec(ctx, `
			UPDATE store_credits
			   SET total_cop = $2, balance_cop = $3, updated_at = now()
			 WHERE sale_id = $1
		`, in.ID, totalCOP, totalCOP-paid); err != nil {
			return sale.Sale{}, fmt.Errorf("actualizar crédito del pedido %d: %w", in.ID, err)
		}
	}

	switch {
	case targetStatus == sale.StatusCancelled:
		if err := cancelPickupTx(ctx, tx, in.ID, reservation.ReturnCancelled); err != nil {
			return sale.Sale{}, err
		}
	case targetStatus == sale.StatusPaid && (currentStatus == sale.StatusPendingPickup || currentStatus == sale.StatusCancelled):
		if _, err := tx.Exec(ctx, `
			UPDATE sales
			   SET status = $2, paid_at = now(), delivered_at = now(), cancel_requested_at = NULL
			 WHERE id = $1
		`, in.ID, sale.StatusPaid); err != nil {
			return sale.Sale{}, fmt.Errorf("marcar pagado el pedido %d: %w", in.ID, err)
		}
		if err := logSaleCardReservationsTx(ctx, tx, in.ID, reservation.LogSold, ""); err != nil {
			return sale.Sale{}, err
		}
		if err := logSaleProductReservationsTx(ctx, tx, in.ID, reservation.LogSold, ""); err != nil {
			return sale.Sale{}, err
		}
	case targetStatus == sale.StatusPendingPickup && currentStatus == sale.StatusCancelled:
		if _, err := tx.Exec(ctx, `
			UPDATE sales
			   SET status = $2, paid_at = NULL, shipped_at = NULL, delivered_at = NULL,
			       cancel_requested_at = NULL, requires_review = false, payment_reference = NULL,
			       bold_payment_id = NULL
			 WHERE id = $1
		`, in.ID, sale.StatusPendingPickup); err != nil {
			return sale.Sale{}, fmt.Errorf("reabrir pedido %d como pendiente: %w", in.ID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return sale.Sale{}, err
	}
	return r.FindByID(ctx, in.ID)
}

func validateOrderTransition(current, target string) error {
	if target == current {
		return nil
	}
	switch current {
	case sale.StatusPendingPickup:
		if target == sale.StatusPaid || target == sale.StatusCancelled {
			return nil
		}
	case sale.StatusPaid:
		if target == sale.StatusCancelled {
			return nil
		}
	case sale.StatusCredit:
		if target == sale.StatusCancelled {
			return nil
		}
	case sale.StatusCancelled:
		if target == sale.StatusPendingPickup {
			return nil
		}
	}
	return sale.ErrOrderNotEditable
}

func updateOrderCardsTx(ctx context.Context, tx pgx.Tx, saleID int64, inputs []sale.OrderItemInput) error {
	rows, err := tx.Query(ctx, `
		SELECT id, listing_id, quantity FROM sale_items WHERE sale_id = $1 FOR UPDATE
	`, saleID)
	if err != nil {
		return fmt.Errorf("leer cartas del pedido %d: %w", saleID, err)
	}
	existing := map[int64]orderCardLine{}
	for rows.Next() {
		var l orderCardLine
		if err := rows.Scan(&l.id, &l.listingID, &l.quantity); err != nil {
			rows.Close()
			return err
		}
		existing[l.id] = l
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	kept := map[int64]bool{}
	for _, in := range inputs {
		if in.Quantity < 0 {
			return sale.ErrInvalidInput
		}
		if in.ID > 0 {
			old, ok := existing[in.ID]
			if !ok {
				return sale.ErrInvalidInput
			}
			kept[in.ID] = true
			if in.Quantity == 0 {
				if err := restoreListingStockTx(ctx, tx, old.listingID, old.quantity); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `DELETE FROM sale_items WHERE id = $1 AND sale_id = $2`, in.ID, saleID); err != nil {
					return fmt.Errorf("eliminar línea %d del pedido %d: %w", in.ID, saleID, err)
				}
				continue
			}
			delta := in.Quantity - old.quantity
			if delta > 0 {
				if old.listingID == nil {
					return sale.ErrInsufficientStock
				}
				if err := takeListingStockTx(ctx, tx, old.listingID, delta); err != nil {
					return err
				}
			} else if delta < 0 {
				if err := restoreListingStockTx(ctx, tx, old.listingID, -delta); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(ctx, `
				UPDATE sale_items
				   SET quantity = $2, price_cop = $3,
				       price_usd = CASE WHEN $4 > 0 THEN $4 ELSE price_usd END
				 WHERE id = $1 AND sale_id = $5
			`, in.ID, in.Quantity, in.PriceCOP, in.PriceUSD, saleID); err != nil {
				return fmt.Errorf("actualizar línea %d del pedido %d: %w", in.ID, saleID, err)
			}
			continue
		}

		if in.ListingID == nil || in.Quantity <= 0 {
			return sale.ErrInvalidInput
		}
		if err := takeListingStockTx(ctx, tx, in.ListingID, in.Quantity); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO sale_items
				(sale_id, listing_id, card_id, card_name, language, quantity, price_cop, price_usd,
				 variant_name, game_code, game_name, expansion_name)
			SELECT $1, il.id, c.id, c.name, il.language, $3, $4, $5,
			       cv.variant_name, g.code, g.name, e.name
			  FROM inventory_listings il
			  JOIN card_variants cv ON cv.id = il.variant_id
			  JOIN cards c ON c.id = cv.card_id
			  JOIN games g ON g.id = c.game_id
			  JOIN expansions e ON e.id = c.expansion_id
			 WHERE il.id = $2
		`, saleID, *in.ListingID, in.Quantity, in.PriceCOP, in.PriceUSD); err != nil {
			return fmt.Errorf("agregar carta al pedido %d: %w", saleID, err)
		}
	}

	for id, old := range existing {
		if kept[id] {
			continue
		}
		if err := restoreListingStockTx(ctx, tx, old.listingID, old.quantity); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM sale_items WHERE id = $1 AND sale_id = $2`, id, saleID); err != nil {
			return fmt.Errorf("eliminar línea %d del pedido %d: %w", id, saleID, err)
		}
	}
	return nil
}

func updateOrderProductsTx(ctx context.Context, tx pgx.Tx, saleID int64, inputs []sale.OrderProductInput) error {
	rows, err := tx.Query(ctx, `
		SELECT id, product_id, quantity FROM sale_product_items WHERE sale_id = $1 FOR UPDATE
	`, saleID)
	if err != nil {
		return fmt.Errorf("leer productos del pedido %d: %w", saleID, err)
	}
	existing := map[int64]orderProductLine{}
	for rows.Next() {
		var l orderProductLine
		if err := rows.Scan(&l.id, &l.productID, &l.quantity); err != nil {
			rows.Close()
			return err
		}
		existing[l.id] = l
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	kept := map[int64]bool{}
	for _, in := range inputs {
		if in.Quantity < 0 {
			return sale.ErrInvalidInput
		}
		if in.ID > 0 {
			old, ok := existing[in.ID]
			if !ok {
				return sale.ErrInvalidInput
			}
			kept[in.ID] = true
			if in.Quantity == 0 {
				if err := restoreProductStockQtyTx(ctx, tx, old.productID, old.quantity); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `DELETE FROM sale_product_items WHERE id = $1 AND sale_id = $2`, in.ID, saleID); err != nil {
					return fmt.Errorf("eliminar producto %d del pedido %d: %w", in.ID, saleID, err)
				}
				continue
			}
			delta := in.Quantity - old.quantity
			if delta > 0 {
				if old.productID == nil {
					return sale.ErrInsufficientStock
				}
				if err := takeProductStockQtyTx(ctx, tx, old.productID, delta); err != nil {
					return err
				}
			} else if delta < 0 {
				if err := restoreProductStockQtyTx(ctx, tx, old.productID, -delta); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(ctx, `
				UPDATE sale_product_items SET quantity = $2, price_cop = $3
				 WHERE id = $1 AND sale_id = $4
			`, in.ID, in.Quantity, in.PriceCOP, saleID); err != nil {
				return fmt.Errorf("actualizar producto %d del pedido %d: %w", in.ID, saleID, err)
			}
			continue
		}

		if in.ProductID == nil || in.Quantity <= 0 {
			return sale.ErrInvalidInput
		}
		if err := takeProductStockQtyTx(ctx, tx, in.ProductID, in.Quantity); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO sale_product_items (sale_id, product_id, title, quantity, price_cop)
			SELECT $1, p.id, p.title, $3, CASE WHEN $4 > 0 THEN $4 ELSE p.price_cop END
			  FROM products p WHERE p.id = $2
		`, saleID, *in.ProductID, in.Quantity, in.PriceCOP); err != nil {
			return fmt.Errorf("agregar producto al pedido %d: %w", saleID, err)
		}
	}

	for id, old := range existing {
		if kept[id] {
			continue
		}
		if err := restoreProductStockQtyTx(ctx, tx, old.productID, old.quantity); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM sale_product_items WHERE id = $1 AND sale_id = $2`, id, saleID); err != nil {
			return fmt.Errorf("eliminar producto %d del pedido %d: %w", id, saleID, err)
		}
	}
	return nil
}

func takeListingStockTx(ctx context.Context, tx pgx.Tx, listingID *int64, quantity int) error {
	if listingID == nil || quantity <= 0 {
		return nil
	}
	tag, err := tx.Exec(ctx, `
		UPDATE inventory_listings
		   SET quantity = quantity - $2,
		       status = CASE WHEN quantity - $2 <= 0 THEN 'inactive' ELSE status END,
		       updated_at = now()
		 WHERE id = $1 AND quantity >= $2
	`, *listingID, quantity)
	if err != nil {
		return fmt.Errorf("descontar stock del listing %d: %w", *listingID, err)
	}
	if tag.RowsAffected() == 0 {
		return sale.ErrInsufficientStock
	}
	return nil
}

func restoreListingStockTx(ctx context.Context, tx pgx.Tx, listingID *int64, quantity int) error {
	if listingID == nil || quantity <= 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE inventory_listings
		   SET quantity = quantity + $2,
		       status = CASE WHEN quantity = 0 AND status = 'inactive' THEN 'active' ELSE status END,
		       updated_at = now()
		 WHERE id = $1
	`, *listingID, quantity); err != nil {
		return fmt.Errorf("devolver stock del listing %d: %w", *listingID, err)
	}
	return nil
}

func takeProductStockQtyTx(ctx context.Context, tx pgx.Tx, productID *int64, quantity int) error {
	if productID == nil || quantity <= 0 {
		return nil
	}
	tag, err := tx.Exec(ctx, `
		UPDATE products SET stock = stock - $2, updated_at = now()
		 WHERE id = $1 AND stock >= $2
	`, *productID, quantity)
	if err != nil {
		return fmt.Errorf("descontar stock del producto %d: %w", *productID, err)
	}
	if tag.RowsAffected() == 0 {
		return sale.ErrInsufficientStock
	}
	return nil
}

func restoreProductStockQtyTx(ctx context.Context, tx pgx.Tx, productID *int64, quantity int) error {
	if productID == nil || quantity <= 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE products SET stock = stock + $2, updated_at = now() WHERE id = $1
	`, *productID, quantity); err != nil {
		return fmt.Errorf("devolver stock del producto %d: %w", *productID, err)
	}
	return nil
}
