package postgres

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"trample-back/internal/domain/sale"
)

// Condiciones SQL de cada etapa del panel de pedidos (alias s = sales). Son la
// única definición de las etapas: las usan el filtro por shipping_status y los
// conteos, para que las tarjetas y el listado no se contradigan.
const (
	stagePendingSQL   = `s.status = 'pending_pickup'`
	stageToShipSQL    = `s.status = 'paid' AND s.fulfillment = 'shipping' AND s.shipped_at IS NULL`
	stageShippedSQL   = `s.status = 'paid' AND s.fulfillment = 'shipping' AND s.shipped_at IS NOT NULL AND s.delivered_at IS NULL`
	stageDeliveredSQL = `s.status = 'paid' AND s.fulfillment = 'shipping' AND s.delivered_at IS NOT NULL`
	stageClosedSQL    = `s.status = 'paid' AND (s.fulfillment = 'pickup' OR s.delivered_at IS NOT NULL)`
	stageCancelledSQL = `s.status = 'cancelled'`
)

// shippingStatusSQL traduce el filtro shipping_status a su condición.
var shippingStatusSQL = map[string]string{
	sale.ShippingPending:   stageToShipSQL,
	sale.ShippingShipped:   stageShippedSQL,
	sale.ShippingDelivered: stageDeliveredSQL,
}

// saleWhere arma el WHERE de los filtros del panel. `withStatus` en false omite
// el filtro de estado (los conteos por etapa lo ignoran a propósito). Los
// valores viajan siempre como parámetros.
func saleWhere(f sale.ListFilter, withStatus bool) (string, []any) {
	var (
		conds []string
		args  []any
	)
	arg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}

	if !f.From.IsZero() {
		conds = append(conds, "s.created_at >= "+arg(f.From))
	}
	if !f.To.IsZero() {
		conds = append(conds, "s.created_at < "+arg(f.To))
	}
	if withStatus && f.Status != "" {
		conds = append(conds, "s.status = "+arg(f.Status))
	}
	if cond, ok := shippingStatusSQL[f.ShippingStatus]; ok {
		conds = append(conds, "("+cond+")")
	}
	if f.Fulfillment != "" {
		conds = append(conds, "s.fulfillment = "+arg(f.Fulfillment))
	}
	if f.PaymentMethod != "" {
		conds = append(conds, "s.payment_method = "+arg(f.PaymentMethod))
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		like := arg("%" + escapeLike(q) + "%")
		parts := []string{
			"trim(concat_ws(' ', u.first_name, u.last_name)) ILIKE " + like,
			"u.email ILIKE " + like,
			"s.phone ILIKE " + like,
			"s.city ILIKE " + like,
			"EXISTS (SELECT 1 FROM sale_items si WHERE si.sale_id = s.id AND si.card_name ILIKE " + like + ")",
			"EXISTS (SELECT 1 FROM sale_product_items sp WHERE sp.sale_id = s.id AND sp.title ILIKE " + like + ")",
		}
		// "#128" o "128" también buscan el número de pedido.
		if id, err := strconv.ParseInt(strings.TrimPrefix(q, "#"), 10, 64); err == nil {
			parts = append(parts, "s.id = "+arg(id))
		}
		conds = append(conds, "("+strings.Join(parts, " OR ")+")")
	}

	if len(conds) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// Search lista las ventas del panel con filtros y devuelve también el total de
// resultados (sin paginar).
func (r *SaleRepository) Search(ctx context.Context, f sale.ListFilter) ([]sale.Sale, int, error) {
	where, args := saleWhere(f, true)

	var total int
	if err := r.db.QueryRow(ctx, `SELECT count(*) FROM sales s LEFT JOIN users u ON u.id = s.user_id`+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("contar ventas: %w", err)
	}

	order := " ORDER BY s.created_at DESC, s.id DESC"
	if f.Ascending {
		order = " ORDER BY s.created_at ASC, s.id ASC"
	}
	n := len(args)
	query := saleSelect + where + order + fmt.Sprintf(" LIMIT $%d OFFSET $%d", n+1, n+2)
	rows, err := r.db.Query(ctx, query, append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("buscar ventas: %w", err)
	}
	var out []sale.Sale
	for rows.Next() {
		s, err := scanSale(rows)
		if err != nil {
			rows.Close()
			return nil, 0, fmt.Errorf("escanear venta: %w", err)
		}
		out = append(out, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	for i := range out {
		if err := r.loadItems(ctx, &out[i]); err != nil {
			return nil, 0, fmt.Errorf("cargar items de la venta %d: %w", out[i].ID, err)
		}
	}
	if err := r.loadCreditsForSales(ctx, out); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// CountByStage cuenta los pedidos de cada etapa con los mismos filtros del
// listado, salvo el de estado.
func (r *SaleRepository) CountByStage(ctx context.Context, f sale.ListFilter) (sale.StageCounts, error) {
	where, args := saleWhere(f, false)
	var c sale.StageCounts
	agg := func(cond string) string {
		return fmt.Sprintf(`count(*) FILTER (WHERE %[1]s)::int, COALESCE(sum(s.total_cop) FILTER (WHERE %[1]s), 0)::bigint`, cond)
	}
	err := r.db.QueryRow(ctx, `
		SELECT `+agg(stagePendingSQL)+`, `+agg(stageToShipSQL)+`, `+agg(stageShippedSQL)+`,
		       `+agg(stageClosedSQL)+`, `+agg(stageCancelledSQL)+`
		FROM sales s LEFT JOIN users u ON u.id = s.user_id`+where, args...).Scan(
		&c.Pending.Orders, &c.Pending.TotalCOP,
		&c.ToShip.Orders, &c.ToShip.TotalCOP,
		&c.Shipped.Orders, &c.Shipped.TotalCOP,
		&c.Closed.Orders, &c.Closed.TotalCOP,
		&c.Cancelled.Orders, &c.Cancelled.TotalCOP,
	)
	if err != nil {
		return c, fmt.Errorf("contar pedidos por etapa: %w", err)
	}
	return c, nil
}
