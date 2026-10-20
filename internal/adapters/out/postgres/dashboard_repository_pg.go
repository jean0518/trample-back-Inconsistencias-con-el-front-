package postgres

import (
	"context"
	"fmt"
	"time"

	"trample-back/internal/domain/dashboard"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DashboardRepository struct {
	db *pgxpool.Pool
}

func NewDashboardRepository(db *pgxpool.Pool) *DashboardRepository {
	return &DashboardRepository{db: db}
}

// InventoryKPIs resuelve los dos indicadores de inventario en una sola pasada.
// Los listings se filtran por status = 'active' porque son los que ve la tienda.
func (r *DashboardRepository) InventoryKPIs(ctx context.Context) (dashboard.KPIs, error) {
	var kpis dashboard.KPIs
	err := r.db.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM inventory_listings WHERE status = 'active'),
			(SELECT COALESCE(sum(quantity), 0) FROM inventory_listings WHERE status = 'active')
	`).Scan(&kpis.ActiveListings, &kpis.InventoryUnits)
	if err != nil {
		return kpis, fmt.Errorf("consultar indicadores de inventario: %w", err)
	}
	return kpis, nil
}

// SalesTotals cuenta los pedidos de la ventana y los dos cortes del panel en
// una sola consulta. Los conteos usan FILTER para no repetir la tabla cuatro
// veces.
func (r *DashboardRepository) SalesTotals(ctx context.Context, since, todayStart time.Time) (dashboard.SalesTotals, error) {
	var t dashboard.SalesTotals
	err := r.db.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE created_at >= $2)::int AS orders_today,
			count(*) FILTER (WHERE created_at >= $3)::int AS orders_last7,
			count(*)::int                                    AS orders,
			COALESCE(sum(total_cop), 0)::bigint              AS total_cop
		FROM sales
		WHERE created_at >= $1 AND status = 'paid'
	`, since, todayStart, todayStart.AddDate(0, 0, -6)).Scan(
		&t.OrdersToday, &t.OrdersLast7, &t.Orders, &t.TotalCOP,
	)
	if err != nil {
		return t, fmt.Errorf("consultar totales de ventas: %w", err)
	}
	return t, nil
}

// SalesSeries agrupa por día en la zona horaria del servidor, que es la misma
// con la que el panel cuenta "pedidos de hoy".
func (r *DashboardRepository) SalesSeries(ctx context.Context, since time.Time) ([]dashboard.DayCount, error) {
	rows, err := r.db.Query(ctx, `
		SELECT
			to_char(date_trunc('day', created_at), 'YYYY-MM-DD')       AS day,
			count(*)::int                                             AS orders,
			COALESCE(sum(total_cop), 0)::bigint                       AS total_cop
		FROM sales
		WHERE created_at >= $1 AND status = 'paid'
		GROUP BY date_trunc('day', created_at)
		ORDER BY date_trunc('day', created_at)
	`, since)
	if err != nil {
		return nil, fmt.Errorf("consultar serie de ventas: %w", err)
	}
	defer rows.Close()

	var out []dashboard.DayCount
	for rows.Next() {
		var d dashboard.DayCount
		if err := rows.Scan(&d.Date, &d.Orders, &d.TotalCOP); err != nil {
			return nil, fmt.Errorf("escanear día de la serie: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// RecentSales une con users para traer el nombre del comprador y cuenta los
// renglones en una subconsulta agregada, para no duplicar el pedido cuando
// tiene varios items.
func (r *DashboardRepository) RecentSales(ctx context.Context, limit int) ([]dashboard.RecentOrder, error) {
	rows, err := r.db.Query(ctx, `
		SELECT
			s.id,
			s.status,
			s.total_cop,
			COALESCE(i.items, 0)::int AS items,
			COALESCE(i.units, 0)::int AS units,
			trim(concat_ws(' ', u.first_name, u.last_name)) AS customer_name,
			to_char(s.created_at, 'YYYY-MM-DD"T"HH24:MI:SS')  AS created_at
		FROM sales s
		LEFT JOIN users u ON u.id = s.user_id
		LEFT JOIN (
			SELECT sale_id, count(*) AS items, sum(quantity) AS units
			FROM (
				SELECT sale_id, quantity FROM sale_items
				UNION ALL
				SELECT sale_id, quantity FROM sale_product_items
			) lines
			GROUP BY sale_id
		) i ON i.sale_id = s.id
		ORDER BY s.created_at DESC, s.id DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("consultar pedidos recientes: %w", err)
	}
	defer rows.Close()

	out := make([]dashboard.RecentOrder, 0, limit)
	for rows.Next() {
		var o dashboard.RecentOrder
		if err := rows.Scan(
			&o.ID, &o.Status, &o.TotalCOP,
			&o.Items, &o.Units, &o.CustomerName, &o.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("escanear pedido reciente: %w", err)
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// BreakdownSales devuelve las ventas creadas en [from, to) y, para las pagadas,
// sus líneas de cartas y de productos. Son dos consultas (encabezados y líneas)
// en vez de una por venta.
func (r *DashboardRepository) BreakdownSales(ctx context.Context, from, to time.Time) ([]dashboard.BreakdownSale, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, status, total_cop, shipping_cop, payment_method, fulfillment, created_at
		FROM sales
		WHERE created_at >= $1 AND created_at < $2
		ORDER BY created_at, id
	`, from, to)
	if err != nil {
		return nil, fmt.Errorf("consultar ventas del desglose: %w", err)
	}
	var (
		sales []dashboard.BreakdownSale
		index = map[int64]int{}
	)
	for rows.Next() {
		var s dashboard.BreakdownSale
		if err := rows.Scan(&s.ID, &s.Status, &s.TotalCOP, &s.ShippingCOP, &s.PaymentMethod, &s.Fulfillment, &s.CreatedAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("escanear venta del desglose: %w", err)
		}
		index[s.ID] = len(sales)
		sales = append(sales, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	lines, err := r.db.Query(ctx, `
		SELECT si.sale_id, false, si.card_id, NULL::bigint, si.card_name, si.language, si.quantity, si.price_cop
		FROM sale_items si JOIN sales s ON s.id = si.sale_id
		WHERE s.status = 'paid' AND s.created_at >= $1 AND s.created_at < $2
		UNION ALL
		SELECT sp.sale_id, true, 0, sp.product_id, sp.title, '', sp.quantity, sp.price_cop
		FROM sale_product_items sp JOIN sales s ON s.id = sp.sale_id
		WHERE s.status = 'paid' AND s.created_at >= $1 AND s.created_at < $2
	`, from, to)
	if err != nil {
		return nil, fmt.Errorf("consultar líneas del desglose: %w", err)
	}
	defer lines.Close()
	for lines.Next() {
		var (
			saleID int64
			l      dashboard.BreakdownLine
		)
		if err := lines.Scan(&saleID, &l.Sealed, &l.CardID, &l.ProductID, &l.Name, &l.Language, &l.Quantity, &l.PriceCOP); err != nil {
			return nil, fmt.Errorf("escanear línea del desglose: %w", err)
		}
		if i, ok := index[saleID]; ok {
			sales[i].Lines = append(sales[i].Lines, l)
		}
	}
	return sales, lines.Err()
}

// SealedProducts devuelve cada producto sellado con sus unidades reservadas
// ahora (mismo criterio que el listado de productos).
func (r *DashboardRepository) SealedProducts(ctx context.Context) ([]dashboard.SealedProduct, error) {
	rows, err := r.db.Query(ctx, `
		SELECT p.id, p.title, p.status = 'active', p.stock,
		       COALESCE((SELECT sum(pr.quantity) FROM product_reservations pr
		                 WHERE pr.product_id = p.id AND pr.status = 'active'), 0)::int,
		       p.price_cop
		FROM products p
		ORDER BY p.title, p.id
	`)
	if err != nil {
		return nil, fmt.Errorf("consultar inventario de sellado: %w", err)
	}
	defer rows.Close()
	var out []dashboard.SealedProduct
	for rows.Next() {
		var p dashboard.SealedProduct
		if err := rows.Scan(&p.ID, &p.Title, &p.Active, &p.Stock, &p.Reserved, &p.PriceCOP); err != nil {
			return nil, fmt.Errorf("escanear producto sellado: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
