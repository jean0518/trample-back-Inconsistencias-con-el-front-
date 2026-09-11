package postgres

import (
	"context"
	"fmt"
	"time"

	"trample-back/internal/domain/sale"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SaleRepository struct {
	db *pgxpool.Pool
}

func NewSaleRepository(db *pgxpool.Pool) *SaleRepository {
	return &SaleRepository{db: db}
}

func (r *SaleRepository) Create(ctx context.Context, s sale.Sale) (sale.Sale, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return sale.Sale{}, err
	}
	defer tx.Rollback(ctx)

	var created sale.Sale
	err = tx.QueryRow(ctx, `
		INSERT INTO sales (user_id, total_cop, total_usd, shipping_cop, shipping_usd, fulfillment, address, city, phone, payment_method, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, user_id, total_cop, total_usd, shipping_cop, shipping_usd, fulfillment, address, city, phone, payment_method, status, created_at
	`, s.UserID, s.TotalCOP, s.TotalUSD, s.ShippingCOP, s.ShippingUSD, s.Fulfillment, s.Address, s.City, s.Phone, s.PaymentMethod, s.Status).Scan(
		&created.ID, &created.UserID, &created.TotalCOP, &created.TotalUSD, &created.ShippingCOP, &created.ShippingUSD,
		&created.Fulfillment, &created.Address, &created.City, &created.Phone,
		&created.PaymentMethod, &created.Status, &created.CreatedAt,
	)
	if err != nil {
		return sale.Sale{}, fmt.Errorf("crear venta: %w", err)
	}

	created.Items = make([]sale.SaleItem, 0, len(s.Items))
	for _, it := range s.Items {
		var item sale.SaleItem
		err := tx.QueryRow(ctx, `
			INSERT INTO sale_items (sale_id, listing_id, card_id, card_name, language, quantity, price_cop, price_usd)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			RETURNING id, sale_id, listing_id, card_id, card_name, language, quantity, price_cop, price_usd
		`, created.ID, it.ListingID, it.CardID, it.CardName, it.Language, it.Quantity, it.PriceCOP, it.PriceUSD).Scan(
			&item.ID, &item.SaleID, &item.ListingID, &item.CardID, &item.CardName,
			&item.Language, &item.Quantity, &item.PriceCOP, &item.PriceUSD,
		)
		if err != nil {
			return sale.Sale{}, fmt.Errorf("crear item de venta: %w", err)
		}
		created.Items = append(created.Items, item)
	}

	// Al vender se descuenta el inventario (la carta se consume aquí, no al
	// reservar): si el listing llega a 0 unidades pasa a 'inactive' y deja de
	// aparecer en el catálogo público; si le quedan unidades, permanece activo
	// y vendible. El detalle de la venta queda en el historial de ventas.
	for _, it := range s.Items {
		if _, err := tx.Exec(ctx, `
			UPDATE inventory_listings
			SET quantity = CASE WHEN quantity - $2 <= 0 THEN 0 ELSE quantity - $2 END,
			    status = CASE WHEN quantity - $2 <= 0 THEN 'inactive' ELSE status END,
			    updated_at = now()
			WHERE id = $1
		`, it.ListingID, it.Quantity); err != nil {
			return sale.Sale{}, fmt.Errorf("descontar listing vendido: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return sale.Sale{}, err
	}
	return created, nil
}

const saleSelect = `
	SELECT id, user_id, total_cop, total_usd, shipping_cop, shipping_usd, fulfillment, address, city, phone, payment_method, status, created_at
	FROM sales
`

func scanSale(row pgx.Row) (sale.Sale, error) {
	var s sale.Sale
	err := row.Scan(
		&s.ID, &s.UserID, &s.TotalCOP, &s.TotalUSD, &s.ShippingCOP, &s.ShippingUSD, &s.Fulfillment, &s.Address,
		&s.City, &s.Phone, &s.PaymentMethod, &s.Status, &s.CreatedAt,
	)
	return s, err
}

func (r *SaleRepository) loadItems(ctx context.Context, s *sale.Sale) error {
	rows, err := r.db.Query(ctx, `
		SELECT id, sale_id, listing_id, card_id, card_name, language, quantity, price_cop, price_usd
		FROM sale_items WHERE sale_id = $1 ORDER BY id
	`, s.ID)
	if err != nil {
		return err
	}
	defer rows.Close()

	var items []sale.SaleItem
	for rows.Next() {
		var it sale.SaleItem
		if err := rows.Scan(&it.ID, &it.SaleID, &it.ListingID, &it.CardID, &it.CardName,
			&it.Language, &it.Quantity, &it.PriceCOP, &it.PriceUSD); err != nil {
			return err
		}
		items = append(items, it)
	}
	s.Items = items
	return rows.Err()
}

func (r *SaleRepository) ListByUser(ctx context.Context, userID int64, limit, offset int) ([]sale.Sale, error) {
	rows, err := r.db.Query(ctx, saleSelect+` WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`, userID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("listar ventas del usuario: %w", err)
	}
	defer rows.Close()

	var out []sale.Sale
	for rows.Next() {
		s, err := scanSale(rows)
		if err != nil {
			return nil, fmt.Errorf("escanear venta: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Carga items de cada venta.
	for i := range out {
		if err := r.loadItems(ctx, &out[i]); err != nil {
			return nil, fmt.Errorf("cargar items de venta %d: %w", out[i].ID, err)
		}
	}
	return out, nil
}

func (r *SaleRepository) ListAll(ctx context.Context, limit, offset int) ([]sale.Sale, error) {
	rows, err := r.db.Query(ctx, saleSelect+` ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("listar ventas: %w", err)
	}
	defer rows.Close()

	var out []sale.Sale
	for rows.Next() {
		s, err := scanSale(rows)
		if err != nil {
			return nil, fmt.Errorf("escanear venta: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if err := r.loadItems(ctx, &out[i]); err != nil {
			return nil, fmt.Errorf("cargar items de venta %d: %w", out[i].ID, err)
		}
	}
	return out, nil
}

// Stats agrega las ventas en buckets (día o semana ISO) desde `since`.
func (r *SaleRepository) Stats(ctx context.Context, period string, since time.Time) ([]sale.StatBucket, error) {
	trunc := "day"
	if period == "week" {
		trunc = "week"
	}

	rows, err := r.db.Query(ctx, `
		SELECT
			to_char(date_trunc($1, created_at), 'YYYY-MM-DD') AS start,
			count(*)::int AS orders,
			COALESCE(sum(total_cop), 0)::bigint AS total_cop,
			COALESCE(sum(total_usd), 0)::numeric AS total_usd
		FROM sales
		WHERE created_at >= $2
		GROUP BY date_trunc($1, created_at)
		ORDER BY date_trunc($1, created_at)
	`, trunc, since)
	if err != nil {
		return nil, fmt.Errorf("agregar ventas: %w", err)
	}
	defer rows.Close()

	var out []sale.StatBucket
	for rows.Next() {
		var b sale.StatBucket
		if err := rows.Scan(&b.Start, &b.Orders, &b.TotalCOP, &b.TotalUSD); err != nil {
			return nil, fmt.Errorf("escanear bucket de ventas: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
